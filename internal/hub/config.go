package hub

// Config is the hub runtime configuration.
type Config struct {
	Addr      string
	DataDir   string
	PublicURL string
	TLSCert   string
	TLSKey    string
	HubName   string
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxDevice
	ctxToken
	ctxStepUp
)
