//go:build myelophone_prod

package goserver

func IsDev() bool                    { return false }
func IsProd() bool                   { return true }
func applicationEnvironment() string { return "prod" }
