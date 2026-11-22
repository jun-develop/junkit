package i18n_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: i18n_test.go
// Purpose: Verify the translator: built-in catalog, default language, adding and resolving messages.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 10
//
// #   Test Case                                              Scenario                                  Expected Result
// 1   TestNewTranslator_SeedsBuiltInCatalog                  New translator                            Default language is en; built-in "required" message resolves
// 2   TestTranslator_SetDefaultLang_ChangesAndResetsDefault  Set vi, then set ""                       Default becomes vi, then resets to en
// 3   TestTranslator_AddTranslations_StoresMessages          Add en and vi messages                    Each language resolves its own message
// 4   TestTranslator_AddTranslations_StoresMessages          Overwrite the en message only             en is replaced; vi is kept
// 5   TestTranslator_GetMessage_ResolvesWithFallbacks        Requested language has the key            Returns the formatted message in that language
// 6   TestTranslator_GetMessage_ResolvesWithFallbacks        Empty language                            Uses the default language
// 7   TestTranslator_GetMessage_ResolvesWithFallbacks        Language without the key                  Falls back to the default language
// 8   TestTranslator_GetMessage_ResolvesWithFallbacks        Unknown key                               Returns the key itself
// 9   TestTranslator_GetMessage_ResolvesWithFallbacks        Message without format verbs, args given  Returns the message unchanged
// 10  TestTranslator_IsSafeForConcurrentUse                  Add and read translations concurrently    No data race (checked by -race)
//
// =============================================================================

import (
	"testing"

	"github.com/jun-develop/junkit/i18n"
)

func TestNewTranslator_SeedsBuiltInCatalog(t *testing.T) {
	tr := i18n.NewTranslator()

	if got := tr.GetDefaultLang(); got != i18n.LangEN {
		t.Fatalf("GetDefaultLang() = %q, want %q", got, i18n.LangEN)
	}

	if got := tr.GetMessage("required", i18n.LangEN, "email"); got != "email is required" {
		t.Fatalf(`GetMessage("required", "en", "email") = %q, want %q`, got, "email is required")
	}
}

func TestTranslator_SetDefaultLang_ChangesAndResetsDefault(t *testing.T) {
	tr := i18n.NewTranslator()

	tr.SetDefaultLang(i18n.LangVI)
	if got := tr.GetDefaultLang(); got != i18n.LangVI {
		t.Fatalf("GetDefaultLang() = %q, want %q", got, i18n.LangVI)
	}

	tr.SetDefaultLang("")
	if got := tr.GetDefaultLang(); got != i18n.LangEN {
		t.Fatalf(`GetDefaultLang() after SetDefaultLang("") = %q, want %q`, got, i18n.LangEN)
	}
}

func TestTranslator_AddTranslations_StoresMessages(t *testing.T) {
	const viMessage = "Không tìm thấy người dùng"

	// newTranslator returns a translator holding user_not_found in en and vi.
	newTranslator := func() *i18n.Translator {
		tr := i18n.NewTranslator()
		tr.AddTranslations(map[string]map[string]string{
			i18n.LangEN: {"user_not_found": "User not found"},
			i18n.LangVI: {"user_not_found": viMessage},
		})

		return tr
	}

	t.Run("add en and vi messages", func(t *testing.T) {
		tr := newTranslator()

		if got := tr.GetMessage("user_not_found", i18n.LangEN); got != "User not found" {
			t.Fatalf("GetMessage(en) = %q, want %q", got, "User not found")
		}

		if got := tr.GetMessage("user_not_found", i18n.LangVI); got != viMessage {
			t.Fatalf("GetMessage(vi) = %q, want %q", got, viMessage)
		}
	})

	t.Run("overwrite the en message only", func(t *testing.T) {
		tr := newTranslator()

		tr.AddTranslations(map[string]map[string]string{
			i18n.LangEN: {"user_not_found": "No such user"},
		})

		if got := tr.GetMessage("user_not_found", i18n.LangEN); got != "No such user" {
			t.Fatalf("GetMessage(en) after overwrite = %q, want %q", got, "No such user")
		}

		if got := tr.GetMessage("user_not_found", i18n.LangVI); got != viMessage {
			t.Fatalf("GetMessage(vi) = %q, want %q (unaffected by an en-only update)", got, viMessage)
		}
	})
}

func TestTranslator_GetMessage_ResolvesWithFallbacks(t *testing.T) {
	tr := i18n.NewTranslator()
	tr.AddTranslations(map[string]map[string]string{
		i18n.LangEN: {"greeting": "Hello, %s!", "static": "a plain message"},
		i18n.LangVI: {"greeting": "Xin chào, %s!"},
	})

	tests := []struct {
		name string
		key  string
		lang string
		args []any
		want string
	}{
		{"requested language has the key", "greeting", i18n.LangVI, []any{"Lan"}, "Xin chào, Lan!"},
		{"empty language uses the default", "greeting", "", []any{"Jane"}, "Hello, Jane!"},
		{"language without the key falls back to the default", "greeting", "fr", []any{"Marie"}, "Hello, Marie!"},
		{"unknown key returns the key itself", "no_such_key", i18n.LangEN, nil, "no_such_key"},
		{"message without format verbs ignores args", "static", i18n.LangEN, []any{"ignored"}, "a plain message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tr.GetMessage(tt.key, tt.lang, tt.args...); got != tt.want {
				t.Fatalf("GetMessage(%q, %q, %v) = %q, want %q", tt.key, tt.lang, tt.args, got, tt.want)
			}
		})
	}
}

// TestTranslator_IsSafeForConcurrentUse has no explicit assertion: run with -race to detect a data race.
func TestTranslator_IsSafeForConcurrentUse(t *testing.T) {
	tr := i18n.NewTranslator()
	done := make(chan struct{})

	go func() {
		for range 100 {
			tr.AddTranslations(map[string]map[string]string{i18n.LangEN: {"k": "v"}})
		}

		close(done)
	}()

	for range 100 {
		tr.GetMessage("k", i18n.LangEN)
	}

	<-done
}
