package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type compatibilityFixtures struct {
	Connection []struct {
		Name        string `json:"name"`
		Output      Port   `json:"output"`
		Input       Port   `json:"input"`
		Connectable bool   `json:"connectable"`
	} `json:"connection"`
	Arity []struct {
		Name        string `json:"name"`
		Multiple    bool   `json:"multiple"`
		Bound       int    `json:"bound"`
		Connectable bool   `json:"connectable"`
	} `json:"arity"`
}

// The canvas runs these same cases in web/src/lib/connections.test.ts. Whichever
// side changes its rules first fails here, instead of shipping a graph the editor
// allows and the server rejects.
func TestPortCompatibilityMatchesTheSharedFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contract", "port-compatibility.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var fixtures compatibilityFixtures
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(fixtures.Connection) == 0 || len(fixtures.Arity) == 0 {
		t.Fatal("fixtures are empty")
	}
	for _, fixture := range fixtures.Connection {
		t.Run(fixture.Name, func(t *testing.T) {
			connectable := Connectable(fixture.Output, fixture.Input) == nil
			if connectable != fixture.Connectable {
				t.Fatalf("Connectable = %v, fixture says %v", connectable, fixture.Connectable)
			}
		})
	}
	for _, fixture := range fixtures.Arity {
		t.Run(fixture.Name, func(t *testing.T) {
			accepted := AcceptsAnother(Port{Multiple: fixture.Multiple}, fixture.Bound)
			if accepted != fixture.Connectable {
				t.Fatalf("AcceptsAnother = %v, fixture says %v", accepted, fixture.Connectable)
			}
		})
	}
}
