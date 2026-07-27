package clientgen

import "testing"

func TestExportedName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"created_at", "CreatedAt"},
		{"id", "ID"},
		{"user_id", "UserID"},
		{"userId", "UserID"},
		{"X-Request-Id", "XRequestID"},
		{"x-trace-id", "XTraceID"},
		{"api_url", "APIURL"},
		{"completed", "Completed"},
		{"title", "Title"},
		{"http_status", "HTTPStatus"},
		{"XMLHttpRequest", "XMLHTTPRequest"},
		{"per-page", "PerPage"},
		{"2fa_enabled", "N2faEnabled"},
		{"", "Field"},
		{"...", "Field"},
	}
	for _, tc := range cases {
		if got := exportedName(tc.in); got != tc.want {
			t.Errorf("exportedName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTypeName_DoesNotRewordComponentNames pins the deliberate difference
// from exportedName: component schema names are already Go type names, so
// splitting them on case transitions would corrupt spellings the author
// chose.
func TestTypeName_DoesNotRewordComponentNames(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Todo", "Todo"},
		{"ProblemDetails", "ProblemDetails"},
		{"OAuth2Config", "OAuth2Config"},
		{"HTTPServer", "HTTPServer"},
		{"lowercase", "Lowercase"},
		{"With.Dots", "WithDots"},
	}
	for _, tc := range cases {
		if got := typeName(tc.in); got != tc.want {
			t.Errorf("typeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMethodName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"getTodosById", "GetTodosById"},
		{"listTodos", "ListTodos"},
		{"list_todos", "ListTodos"},
		{"get-todo-by-id", "GetTodoByID"},
		{"ListTodos", "ListTodos"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := methodName(tc.in); got != tc.want {
			t.Errorf("methodName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEnsureIdentifier_AvoidsKeywordsAndBadStarts(t *testing.T) {
	cases := []struct{ in, want string }{
		{"type", "type_"},
		{"range", "range_"},
		{"9lives", "N9lives"},
		{"", "Field"},
		{"ok", "ok"},
	}
	for _, tc := range cases {
		if got := ensureIdentifier(tc.in); got != tc.want {
			t.Errorf("ensureIdentifier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNameSet_ResolvesCollisionsDeterministically matters because a
// collision resolved differently across runs would break the drift check.
func TestNameSet_ResolvesCollisionsDeterministically(t *testing.T) {
	for range 20 {
		s := newNameSet(map[string]bool{"Client": true})
		got := []string{
			s.claim("Todo"),
			s.claim("Todo"),
			s.claim("Todo"),
			s.claim("Client"),
		}
		want := []string{"Todo", "Todo2", "Todo3", "Client2"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("claim %d: got %q, want %q", i, got[i], want[i])
			}
		}
	}
}

// TestNameSet_ReservedRuntimeNamesAreNotHandedOut prevents a schema named
// "Client" or "APIError" from producing a package that cannot compile.
func TestNameSet_ReservedRuntimeNamesAreNotHandedOut(t *testing.T) {
	s := newNameSet(reservedNames)
	for name := range reservedNames {
		if got := s.claim(name); got == name {
			t.Errorf("claim(%q) handed back the reserved runtime name", name)
		}
	}
}
