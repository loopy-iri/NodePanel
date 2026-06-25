package api

import "testing"

func TestGRPCAddressFor(t *testing.T) {
	cases := []struct {
		addr string
		port int
		want string
	}{
		{"https://1.2.3.4:8090", 62050, "1.2.3.4:62050"},
		{"http://node.example", 62051, "node.example:62051"},
		{"1.2.3.4:8090", 0, "1.2.3.4:62050"},
		{"1.2.3.4", 62050, "1.2.3.4:62050"},
	}
	for _, c := range cases {
		if got := grpcAddressFor(c.addr, c.port); got != c.want {
			t.Errorf("grpcAddressFor(%q,%d) = %q, want %q", c.addr, c.port, got, c.want)
		}
	}
}
