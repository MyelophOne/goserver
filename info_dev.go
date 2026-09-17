//go:build !myelophone_prod

package goserver

func IsDev() bool {
	return AppEnv == "dev"
}

func IsProd() bool {
	return AppEnv == "prod"
}

func applicationEnvironment() string { return GetEnv("APP_ENV", "dev") }
