package paginate

import "testing"

func TestConfigField(t *testing.T) {
	t.Parallel()
	cfg := Config{Fields: []Field{{Name: "id", Column: "users.id"}, {Name: "status", Column: "users.status"}}}
	tests := []struct {
		name  string
		field string
		want  *Field
	}{
		{name: "first field", field: "id", want: &cfg.Fields[0]},
		{name: "another field", field: "status", want: &cfg.Fields[1]},
		{name: "no such field", field: "secret"},
		{name: "names are case-sensitive", field: "Status"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cfg.Field(tc.field); got != tc.want {
				t.Fatalf("want %p, got %p", tc.want, got)
			}
		})
	}
}

func TestConfigFieldChangesTheConfig(t *testing.T) {
	t.Parallel()
	cfg := Config{Fields: []Field{{Name: "status", Column: "users.status"}}}
	cfg.Field("status").Type = Enum("active", "blocked")
	if got := cfg.Fields[0].Type.String(); got != "enum(active|blocked)" {
		t.Fatalf("the change must reach the config, got %s", got)
	}
}
