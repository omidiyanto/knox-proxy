package credential

// TeamCredential represents n8n local account credentials for a team.
type TeamCredential struct {
	Email    string
	Password string
}

// CredentialProvider provides team credentials for n8n account lookups.
type CredentialProvider interface {
	// GetCredential returns the n8n credentials for a given team name.
	GetCredential(teamName string) (*TeamCredential, error)

	// ListTeams returns all available team names.
	ListTeams() []string
}
