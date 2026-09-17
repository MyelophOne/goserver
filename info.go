package goserver

var (
	AppVersion = "dev"
	AppEnv     string
)

func init() {
	if AppEnv == "" {
		AppEnv = applicationEnvironment()
	}
}
