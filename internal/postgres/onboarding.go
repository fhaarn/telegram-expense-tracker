package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"telegram-expense-tracker/internal/user"
	"time"
)

type Onboarding struct {
	Pool                            *pgxpool.Pool
	Timezone, Currency, BotUsername string
}

// Accept commits the inbound ID, profile transition and reply together. A failed
// transaction leaves no acknowledged work; Telegram can safely retry it.
func (s *Onboarding) Accept(ctx context.Context, m user.Message) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(7312041)"); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, "INSERT INTO inbound_updates(update_id) VALUES($1) ON CONFLICT DO NOTHING", m.UpdateID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	// Check before the profile upsert so blocked updates cannot alter metadata.
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE telegram_user_id=$1 AND access_status='blocked')`, m.TelegramID).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		if _, err = tx.Exec(ctx, `UPDATE inbound_updates SET user_id=(SELECT id FROM users WHERE telegram_user_id=$2) WHERE update_id=$1`, m.UpdateID, m.TelegramID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var p user.Profile
	var timezone string
	var smoker *bool
	err = tx.QueryRow(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id,telegram_username,timezone,currency)
 VALUES($1,$2,NULLIF($3,''),$4,$5) ON CONFLICT(telegram_user_id) DO UPDATE
 SET telegram_chat_id=excluded.telegram_chat_id,telegram_username=excluded.telegram_username,updated_at=now()
 RETURNING id,COALESCE(display_name,''),status='active',timezone,is_smoker`, m.TelegramID, m.ChatID, m.Username, s.Timezone, s.Currency).Scan(&p.ID, &p.Name, &p.Active, &timezone, &smoker)
	if err != nil {
		return err
	}
	// Bound bursts per registered identity without retaining message content.
	var allowed bool
	err = tx.QueryRow(ctx, `UPDATE users SET request_count=CASE WHEN request_window < now()-interval '1 minute' THEN 1 ELSE request_count+1 END, request_window=CASE WHEN request_window < now()-interval '1 minute' THEN now() ELSE request_window END WHERE id=$1 RETURNING request_count<=60`, p.ID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		if _, err = tx.Exec(ctx, `INSERT INTO notification_outbox(update_id,user_id,chat_id,body) SELECT $1,id,telegram_chat_id,'⏳ Lots of requests! Please wait a minute before trying again.' FROM users WHERE id=$2 AND request_count=61`, m.UpdateID, p.ID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE inbound_updates SET user_id=$2 WHERE update_id=$1", m.UpdateID, p.ID); err != nil {
			return err
		}
		// A webhook response acknowledges excess requests; avoid creating an unbounded reply backlog.
		return tx.Commit(ctx)
	}
	// The upsert locks the user's row, serializing this user's state transitions.
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_interactions WHERE user_id=$1 AND kind='onboarding')", p.ID).Scan(&p.AwaitingName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(m.Text) == "/cancel" {
		if _, err = tx.Exec(ctx, "UPDATE users SET pending_comparison_hash=NULL WHERE id=$1", p.ID); err != nil {
			return err
		}
	}
	original := m
	fields := strings.Fields(m.Text)
	invite := len(fields) == 2 && strings.SplitN(fields[0], "@", 2)[0] == "/start" && strings.HasPrefix(fields[1], "compare_")
	if invite && !p.Active {
		if err = SavePendingInvite(ctx, tx, p.ID, strings.TrimPrefix(fields[1], "compare_")); err != nil {
			return err
		}
		m.Text = "/start"
	}
	decision := user.Respond(p, m)
	reply := user.Reply{Text: decision.Reply}
	if p.Active {
		command := ""
		if len(fields) > 0 {
			command = strings.SplitN(fields[0], "@", 2)[0]
		}
		if m.Callback == "profile:smoker:yes" || m.Callback == "profile:smoker:no" {
			if smoker != nil {
				reply = user.Reply{Text: "👌 Your preference is already saved."}
			} else {
				_, err = tx.Exec(ctx, "UPDATE users SET is_smoker=$2,updated_at=now() WHERE id=$1 AND is_smoker IS NULL", p.ID, m.Callback == "profile:smoker:yes")
				reply = user.Reply{Text: "✅ Preference saved!\nSend an expense like bensin 100k, or /help to see how to use the bot."}
				if err == nil {
					var pending user.Reply
					pending, err = PendingInviteReply(ctx, tx, p.ID)
					if pending.Text != "" {
						reply = pending
					}
				}
			}
		} else if command == "/start" && !invite && smoker == nil {
			reply = smokingQuestion()
		} else if strings.HasPrefix(m.Callback, "c:") || command == "/compare" || command == "/disconnect" || invite {
			reply, err = HandleComparison(ctx, tx, p.ID, p.Name, timezone, s.BotUsername, original)
		} else if command != "/start" && command != "/help" {
			reply, err = HandleExpense(ctx, tx, p.ID, p.Name, timezone, m)
		}
		decision.Begin = false
		decision.Cancel = false
		decision.Activate = false
		if err != nil {
			return err
		}
	}

	if decision.Begin {
		_, err = tx.Exec(ctx, "INSERT INTO user_interactions(user_id,kind) VALUES($1,'onboarding') ON CONFLICT DO NOTHING", p.ID)
	}
	if err != nil {
		return err
	}
	if decision.Cancel || decision.Activate {
		_, err = tx.Exec(ctx, "DELETE FROM user_interactions WHERE user_id=$1", p.ID)
	}
	if err != nil {
		return err
	}
	if decision.Activate {
		if _, err = tx.Exec(ctx, "UPDATE users SET display_name=$2,status='active',updated_at=now() WHERE id=$1", p.ID, decision.Name); err != nil {
			return err
		}
		for _, category := range user.StarterCategories {
			if _, err = tx.Exec(ctx, "INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", p.ID, category, strings.ToLower(category)); err != nil {
				return err
			}
		}
	}
	if decision.Activate {
		reply = smokingQuestion()
		reply.Text = "✅ Nice to meet you, " + decision.Name + "!\n\n" + reply.Text + "\n\n💡 Send /help to see the commands and how to use the bot."
	}

	if _, err = tx.Exec(ctx, "UPDATE inbound_updates SET user_id=$2 WHERE update_id=$1", m.UpdateID, p.ID); err != nil {
		return err
	}
	markup, e := json.Marshal(reply.Buttons)
	if e != nil {
		return e
	}
	if _, err = tx.Exec(ctx, "INSERT INTO notification_outbox(update_id,user_id,chat_id,body,reply_markup,pair_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,0))", m.UpdateID, p.ID, m.ChatID, reply.Text, markup, reply.PairID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func smokingQuestion() user.Reply {
	return user.Reply{Text: "🚬 Do you smoke or vape?", Buttons: [][]user.Button{{{Text: "Yes", Data: "profile:smoker:yes"}, {Text: "No", Data: "profile:smoker:no"}}}}
}
