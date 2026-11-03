package rhx

// Each fixture owns its store; it never invokes native credential tools.
type recordingCredentialStore struct {
	username, password string
	apiKey, privateKey string
	ops                []string
}

func (s *recordingCredentialStore) brokerageCredentials(profile string) (string, string, error) {
	s.ops = append(s.ops, "read brokerage:"+profile)
	u, p := envOrNone("RH_USERNAME"), envOrNone("RH_PASSWORD")
	if u == "" {
		u = s.username
	}
	if p == "" {
		p = s.password
	}
	return u, p, nil
}
func (s *recordingCredentialStore) saveBrokerageCredentials(profile, username, password string) error {
	s.ops = append(s.ops, "save brokerage:"+profile)
	s.username, s.password = username, password
	return nil
}
func (s *recordingCredentialStore) deleteBrokerageCredentials(profile string) {
	s.ops = append(s.ops, "delete brokerage:"+profile)
	s.username, s.password = "", ""
}
func (s *recordingCredentialStore) cryptoCredentials(profile string) (string, string, error) {
	s.ops = append(s.ops, "read crypto:"+profile)
	u, p := envOrNone("RH_CRYPTO_API_KEY"), envOrNone("RH_CRYPTO_PRIVATE_KEY_B64")
	if u == "" {
		u = s.apiKey
	}
	if p == "" {
		p = s.privateKey
	}
	return u, p, nil
}
func (s *recordingCredentialStore) deleteCryptoCredentials(profile string) {
	s.ops = append(s.ops, "delete crypto:"+profile)
	s.apiKey, s.privateKey = "", ""
}
