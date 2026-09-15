// Package user defines onboarding rules independently of Telegram and PostgreSQL.
package user

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Message struct {
	UpdateID, TelegramID, ChatID int64
	Username, Text, Callback     string
	Image                        bool
}
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}
type Reply struct {
	SavedExpenseID int64 // Internal marker for a newly saved expense; never set for edits.
	PairID         int64
	Text           string
	Buttons        [][]Button
}

type Profile struct {
	ID                   int64
	Name                 string
	Active, AwaitingName bool
}
type Decision struct {
	Reply, Name             string
	Begin, Cancel, Activate bool
}

var StarterCategories = []string{"Food & drinks", "Transport", "Groceries", "Shopping", "Bills", "Entertainment", "Health", "Other"}

const Prompt = "👋 Hey! What should I call you?"
const Help = `🧾 Expense tracker help

💸 Add an expense
• Send a description with the amount at the end.
• Example: bensin 100k
• Example: Nice 8 Ball Cafe 100k

📋 Commands
• /today — Today's expenses and category totals
• /month — This month's total and category breakdown
• /recent — List expenses with Edit and Delete buttons
• /compare — Invite a partner or view your comparison
• /disconnect — Disconnect from your partner
• /cancel — Cancel the current entry or action
• /start — Set up your profile or return to the bot
• /help — Show this guide

✏️ Edit an expense
• Open /recent → tap Edit beside the expense.
• Tap Edit in the preview and choose a field.
• Make your change, then tap Save.`

func NormalizeName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", errors.New("invalid name")
	}
	for _, c := range raw {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return "", errors.New("name must be a single line")
		}
	}
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return "", errors.New("name must contain 1 to 40 characters")
	}
	return name, nil
}

func Respond(p Profile, m Message) Decision {
	text := strings.TrimSpace(m.Text)
	fields := strings.Fields(text)
	command := ""
	if len(fields) > 0 && strings.HasPrefix(fields[0], "/") {
		command = strings.SplitN(fields[0], "@", 2)[0]
	}
	switch command {
	case "/start":
		if len(fields) > 1 {
			return Decision{Reply: "🤝 Comparison invites are not available yet. Send /start to register."}
		}
		if p.Active {
			return Decision{Reply: "👋 Welcome back, " + p.Name + "!\n" + Help}
		}
		return Decision{Reply: Prompt, Begin: true}
	case "/help":
		return Decision{Reply: Help}
	case "/cancel":
		return Decision{Reply: "👌 Cancelled. Send /start whenever you’re ready.", Cancel: true}
	}
	if command != "" {
		return Decision{Reply: "🤔 That command isn’t available yet. Try /start or /help."}
	}
	if !p.Active {
		if !p.AwaitingName {
			return Decision{Reply: Prompt, Begin: true}
		}
		if text == "" {
			return Decision{Reply: "🏷️ Please send your name as text."}
		}
		name, err := NormalizeName(m.Text)
		if err != nil {
			return Decision{Reply: "🤔 Please use a name with 1–40 characters on one line."}
		}
		return Decision{Name: name, Activate: true, Reply: "✅ Nice to meet you, " + name + "!\nSend your first expense, like bensin 100k.\n\n💡 Send /help to see the commands and how to use the bot."}
	}
	if m.Image {
		return Decision{Reply: "🧾 Screenshot reading is coming in phase 2."}
	}
	return Decision{Reply: "🚧 Expense tracking is coming next. Your profile is saved!"}
}
