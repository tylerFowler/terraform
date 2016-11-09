package template

import "testing"

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
			t.Errorf("Expected warning to be given for host %v but was given none", tc.EtcHost)
		} else if !tc.ExpectsWarning && len(warnings) > 0 {
			t.Errorf("Expected no warning to be given for host %v but was given warnings %v", tc.EtcHost, warnings)
		}
	}
}
