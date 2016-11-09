package template

import (
	"testing"

	"github.com/hashicorp/terraform/helper/schema"
)

// Validation Func Tests
func TestEtcHostsValidation(t *testing.T) {
	testCases := []struct {
		EtcHost        string
		ExpectsWarning bool
	}{
		{EtcHost: "localhost", ExpectsWarning: false},
		{EtcHost: "", ExpectsWarning: true},
		{EtcHost: "10.0.0.15", ExpectsWarning: true},
	}

	for _, tc := range testCases {
		warnings, _ := etcHostsValidation(tc.EtcHost, "")

		if tc.ExpectsWarning && len(warnings) == 0 {
			t.Errorf("Expected warning to be given for host %v but none was given", tc.EtcHost)
		} else if !tc.ExpectsWarning && len(warnings) > 0 {
			t.Errorf("Expected no warning to be given for host %v but was given warnings %v", tc.EtcHost, warnings)
		}
	}
}

func TestLocksmithRebootStrategy(t *testing.T) {
	testCases := []struct {
		RebootStrategy string
		ExpectsError   bool
	}{
		{RebootStrategy: "reboot", ExpectsError: false},
		{RebootStrategy: "etcd-lock", ExpectsError: false},
		{RebootStrategy: "best-effort", ExpectsError: false},
		{RebootStrategy: "off", ExpectsError: false},
		{RebootStrategy: "", ExpectsError: true},
		{RebootStrategy: "not-a-strategy", ExpectsError: false},
	}

	for _, tc := range testCases {
		_, errors := updateSchema.Elem.(*schema.Resource).Schema["reboot_strategy"].ValidateFunc(tc.RebootStrategy, "")

		if tc.ExpectsError && len(errors) == 0 {
			t.Errorf("Expected error to be given for reboot strategy %v but none was given", tc.RebootStrategy)
		} else if !tc.ExpectsError && len(errors) > 0 {
			t.Errorf("Expected no error to be given for reboot strategy %v but was given errors %v", tc.RebootStrategy, errors)
		}
	}
}
