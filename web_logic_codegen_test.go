package goserver

import (
	"strings"
	"testing"

	runtime "github.com/myelophone/goserver/web/runtime"
)

func TestGeneratedWebLogicBindingRegistersReferencedHandler(t *testing.T) {
	handler, ok := runtime.Resolve("ServerDataPage")
	if !ok {
		t.Fatal("generated binding did not register ServerDataPage")
	}
	data, err := handler.Render(&runtime.Context{}, runtime.Props{})
	if err != nil || data["message"] != "This value came from web/modules." {
		t.Fatalf("handler data=%#v err=%v", data, err)
	}
}

func TestRunWebCLIIsDisabledInProduction(t *testing.T) {
	previous := AppEnv
	AppEnv = "prod"
	t.Cleanup(func() { AppEnv = previous })
	if err := RunWebCLI([]string{"generate"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("RunWebCLI() error = %v, want production-disabled error", err)
	}
}
