package goserver

var (
	AppVersion = "dev"
	AppEnv     string
)

func IsDev() bool {
	return AppEnv == "dev"
}

func IsProd() bool {
	return AppEnv == "prod"
}

func init() {
	if AppEnv == "" {
		AppEnv = GetEnv("APP_ENV", "dev")
	}
}
