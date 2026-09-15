package user

import "testing"

func TestNames(t *testing.T) {
	for _, tt := range []struct {
		raw, want string
		bad       bool
	}{
		{"  Farhan   A  ", "Farhan A", false}, {"🌻 Budi", "🌻 Budi", false}, {"<b>Farhan</b>", "<b>Farhan</b>", false},
		{"", "", true}, {"Farhan\nBudi", "", true}, {"Farhan\tBudi", "", true}, {"12345678901234567890123456789012345678901", "", true},
	} {
		got, err := NormalizeName(tt.raw)
		if (err != nil) != tt.bad || got != tt.want {
			t.Errorf("name validation mismatch for %q", tt.raw)
		}
	}
}
func TestConversation(t *testing.T) {
	p := Profile{}
	d := Respond(p, Message{Text: "/start"})
	if !d.Begin || d.Activate {
		t.Fatal("expected name prompt")
	}
	p.AwaitingName = true
	d = Respond(p, Message{Text: "/help"})
	if d.Activate || d.Cancel {
		t.Fatal("help must preserve onboarding")
	}
	d = Respond(p, Message{Text: "Farhan"})
	if !d.Activate || d.Name != "Farhan" {
		t.Fatal("expected registration")
	}
	p = Profile{Active: true, Name: "Farhan"}
	d = Respond(p, Message{Text: "/start"})
	if d.Activate || d.Begin {
		t.Fatal("returning start must not reset profile")
	}
	d = Respond(p, Message{Text: "Budi"})
	if d.Activate {
		t.Fatal("active profile must not be renamed by expense text")
	}
	d = Respond(Profile{AwaitingName: true}, Message{Text: "/cancel"})
	if !d.Cancel {
		t.Fatal("cancel")
	}
	d = Respond(Profile{}, Message{Text: "/start compare_abc"})
	if d.Activate || d.Begin {
		t.Fatal("unsupported invite must not be consumed as a name")
	}
}
