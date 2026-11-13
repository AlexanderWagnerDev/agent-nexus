package hub

// Config is the hub runtime configuration.
type Config struct {
	Addr      string
	DataDir   string
	PublicURL string
	TLSCert   string
	TLSKey    string
	HubName   string
	// MetricsEnabled exposes /metrics. MetricsToken is the required bearer
	// token; an empty token disables the endpoint even when enabled is true.
	MetricsEnabled bool
	MetricsToken   string
	// Concurrency limits for new runs. Zero disables the corresponding check.
	MaxRunsPerHost    int
	MaxRunsPerUser    int
	MaxRunsPerProject int
	// RequireSignedCommands rejects prompt/approval/stop writes unless the
	// browser or native client signs them with a registered device key.
	RequireSignedCommands bool
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxDevice
	ctxToken
	ctxStepUp
	ctxRequestID
)
