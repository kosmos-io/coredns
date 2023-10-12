package test

import (
	"strings"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/netmap"
)

func TestNetMap(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		cidrsMap map[string]string
		want     string
	}{
		{
			name: "ipv4-1",
			ip:   "10.234.0.1",
			cidrsMap: map[string]string{
				"10.234.0.0/16": "10.238.0.0/16",
			},
			want: "10.238.0.1",
		},
		{
			name: "ipv4-2",
			ip:   "10.234.12.13",
			cidrsMap: map[string]string{
				"10.232.0.0/16": "10.239.0.0/16",
				"10.234.0.0/16": "10.238.0.0/16",
			},
			want: "10.238.12.13",
		},
		{
			name: "ipv4-3",
			ip:   "10.233.12.13",
			cidrsMap: map[string]string{
				"10.234.0.0/16": "10.238.0.0/16",
			},
			want: "10.233.12.13",
		},
		{
			name: "ipv4-4",
			ip:   "10.234.12.13",
			cidrsMap: map[string]string{
				"10.234.0.0/16": "10.238.0.0/18",
			},
			want: "",
		},
		{
			name: "ipv4-5",
			ip:   "10.234.12.13",
			cidrsMap: map[string]string{
				"10.234.0.0/16": "10.238.1.2/16",
			},
			want: "10.238.12.13",
		},
		{
			name: "ipv4-6",
			ip:   "None",
			cidrsMap: map[string]string{
				"10.234.0.0/16": "10.238.0.0/18",
			},
			want: "None",
		},
		{
			name: "ipv6-1",
			ip:   "2001:0:0:CD30::1",
			cidrsMap: map[string]string{
				"2001:0:0:CD30::/60": "4292:0:0:CD30::/60",
			},
			want: "4292:0:0:CD30::1",
		},
		{
			name: "ipv6-2",
			ip:   "2001:0:0:CD30::1",
			cidrsMap: map[string]string{
				"2001:0:0:CD30::/60": "4292:0:0:CD30::/30",
			},
			want: "",
		},
		{
			name: "ipv6-3",
			ip:   "2001:0:0:CD30::1",
			cidrsMap: map[string]string{
				"2001:0:0:CD30::/60": "4292:0:0:CD30::3/60",
			},
			want: "4292:0:0:CD30::1",
		},
		{
			name: "ipv4+ipv6-1",
			ip:   "2001:0:0:CD30::1",
			cidrsMap: map[string]string{
				"10.234.0.0/16":      "10.238.1.2/16",
				"2001:0:0:CD30::/60": "4292:0:0:CD30::3/60",
			},
			want: "4292:0:0:CD30::1",
		},
		{
			name: "ipv4+ipv6-2",
			ip:   "10.222.222.222",
			cidrsMap: map[string]string{
				"10.234.0.0/16":      "10.238.1.2/16",
				"2001:0:0:CD30::/60": "4292:0:0:CD30::3/60",
				"10.222.0.0/16":      "10.234.1.2/16",
			},
			want: "10.234.222.222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := netmap.NetMap(tt.ip, tt.cidrsMap); !strings.EqualFold(got, tt.want) {
				t.Errorf("kubernetes.NetMap() = %v, want %v", got, tt.want)
			}
		})
	}
}
