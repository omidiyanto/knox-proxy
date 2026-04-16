package credential

import (
	"testing"
)

func TestEnvProvider_GetCredential(t *testing.T) {
	teams := map[string]TeamCredential{
		"finance":   {Email: "fin@local", Password: "finPass"},
		"developer": {Email: "dev@local", Password: "devPass"},
	}
	p := NewEnvProvider(teams)

	cred, err := p.GetCredential("finance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cred.Email != "fin@local" {
		t.Errorf("expected fin@local, got %s", cred.Email)
	}
	if cred.Password != "finPass" {
		t.Errorf("expected finPass, got %s", cred.Password)
	}
}

func TestEnvProvider_GetCredential_NotFound(t *testing.T) {
	p := NewEnvProvider(map[string]TeamCredential{})

	_, err := p.GetCredential("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent team")
	}
}

func TestEnvProvider_ListTeams(t *testing.T) {
	teams := map[string]TeamCredential{
		"finance":   {Email: "fin@local", Password: "finPass"},
		"developer": {Email: "dev@local", Password: "devPass"},
		"ops":       {Email: "ops@local", Password: "opsPass"},
	}
	p := NewEnvProvider(teams)

	teamList := p.ListTeams()
	if len(teamList) != 3 {
		t.Errorf("expected 3 teams, got %d", len(teamList))
	}

	// Verify all teams are present
	teamSet := make(map[string]bool)
	for _, name := range teamList {
		teamSet[name] = true
	}
	for name := range teams {
		if !teamSet[name] {
			t.Errorf("expected team %s in list", name)
		}
	}
}

func TestEnvProvider_MapCopyIsolation(t *testing.T) {
	original := map[string]TeamCredential{
		"team1": {Email: "a@local", Password: "pass"},
	}
	p := NewEnvProvider(original)

	// Mutate original map
	original["team2"] = TeamCredential{Email: "b@local", Password: "pass"}

	// Provider should not be affected
	_, err := p.GetCredential("team2")
	if err == nil {
		t.Error("expected error: provider should be isolated from original map mutation")
	}

	teams := p.ListTeams()
	if len(teams) != 1 {
		t.Errorf("expected 1 team, got %d", len(teams))
	}
}

func TestEnvProvider_EmptyMap(t *testing.T) {
	p := NewEnvProvider(nil)

	teams := p.ListTeams()
	if len(teams) != 0 {
		t.Errorf("expected 0 teams, got %d", len(teams))
	}
}
