package credential

import "fmt"

// EnvProvider provides team credentials from static environment variable configuration.
type EnvProvider struct {
	teams map[string]TeamCredential
}

// NewEnvProvider creates a new EnvProvider from a pre-parsed credential map.
func NewEnvProvider(teams map[string]TeamCredential) *EnvProvider {
	// Copy the map to prevent external mutation
	copied := make(map[string]TeamCredential, len(teams))
	for k, v := range teams {
		copied[k] = v
	}
	return &EnvProvider{teams: copied}
}

// GetCredential returns credentials for the given team.
func (p *EnvProvider) GetCredential(teamName string) (*TeamCredential, error) {
	cred, ok := p.teams[teamName]
	if !ok {
		return nil, fmt.Errorf("team not found: %s", teamName)
	}
	return &cred, nil
}

// ListTeams returns all configured team names.
func (p *EnvProvider) ListTeams() []string {
	teams := make([]string, 0, len(p.teams))
	for k := range p.teams {
		teams = append(teams, k)
	}
	return teams
}
