package api

import "testing"

func TestGRPCAddressFor(t *testing.T) {
	cases := map[string]string{
		"https://1.2.3.4:8090": "1.2.3.4:62050",
		"http://node.example":  "node.example:62050",
		"1.2.3.4:8090":         "1.2.3.4:62050",
		"1.2.3.4":              "1.2.3.4:62050",
	}
	for in, want := range cases {
		if got := grpcAddressFor(in); got != want {
			t.Errorf("grpcAddressFor(%q) = %q, want %q", in, got, want)
		}
	}
}
