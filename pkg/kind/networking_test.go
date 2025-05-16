package kind

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_parseDockerV4Network(t *testing.T) {
	testCases := []struct {
		desc        string
		jsonData    string
		expectedNet net.IPNet
		expectedErr error
	}{
		{
			desc:        "should find v4 network",
			jsonData:    `[{"Name":"kind","Id":"12da2f79f0833bc2f200a19430f0681ebcb34172f31c4e36be5fc1b98baa0cbc","Created":"2023-05-30T11:29:09.1977428+02:00","Scope":"local","Driver":"bridge","EnableIPv6":true,"IPAM":{"Driver":"default","Options":{},"Config":[{"Subnet":"172.19.0.0/16","Gateway":"172.19.0.1"},{"Subnet":"fc00:f853:ccd:e793::/64","Gateway":"fc00:f853:ccd:e793::1"}]},"Internal":false,"Attachable":false,"Ingress":false,"ConfigFrom":{"Network":""},"ConfigOnly":false,"Containers":{"6f2a311eac05dd159140c280f38b28f4af5fac24966619ae67351a04ac0b0872":{"Name":"kube-system.three-control-plane","EndpointID":"49c7b225fd21d6103458cb44fdbba915eaf078ff87f8721c88bee5048c404e58","MacAddress":"02:42:ac:13:00:04","IPv4Address":"172.19.0.4/16","IPv6Address":"fc00:f853:ccd:e793::4/64"},"9ac1ca74027bed08fd22d325352d1d5fa65478912c98de9c3e322ccaacd5ac2d":{"Name":"default.one-control-plane","EndpointID":"bb99a7571e0d0e6e8ae39c9c2b1e7649f766872035668665e5465d8b3d72aaa7","MacAddress":"02:42:ac:13:00:03","IPv4Address":"172.19.0.3/16","IPv6Address":"fc00:f853:ccd:e793::3/64"},"af9a154989d0ce28dfcf9fc38a9f377fcb9cdd8d0d74ba92260ed2e2bcb43e0e":{"Name":"kind-control-plane","EndpointID":"f70b3da9503ff6abae8a8e51fa7eabf20ca764012bf2a5d20ce1b82a2d928195","MacAddress":"02:42:ac:13:00:05","IPv4Address":"172.19.0.5/16","IPv6Address":"fc00:f853:ccd:e793::5/64"},"fc752ade5c09e3d4f45f2bf498a7ed4c2a06dc451be417ebda109f862317293a":{"Name":"default.two-control-plane","EndpointID":"8fcd5372145cfe0a7705a9e0d8bafa1fa9c5fdaed94528434e6a74a3f09733bd","MacAddress":"02:42:ac:13:00:02","IPv4Address":"172.19.0.2/16","IPv6Address":"fc00:f853:ccd:e793::2/64"}},"Options":{"com.docker.network.bridge.enable_ip_masquerade":"true","com.docker.network.driver.mtu":"1500"},"Labels":{}}]`,
			expectedNet: mustParseCIDR("172.19.0.0/16"),
			expectedErr: nil,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			actualNet, actualErr := parseDockerV4Network([]byte(tC.jsonData))
			assertEqualIPNet(t, actualNet, tC.expectedNet)
			assert.Equal(t, tC.expectedErr, actualErr)
		})
	}
}

func Test_calculateV4Subnet(t *testing.T) {
	testCases := []struct {
		desc        string
		input       net.IPNet
		offset      int
		expected    net.IPNet
		expectedErr error
	}{
		{
			desc:     "should return subnet for /16 network",
			input:    mustParseCIDR("172.19.0.0/16"),
			offset:   200,
			expected: mustParseCIDR("172.19.200.0/24"),
		},
		{
			desc:     "should return subnet for /8 network",
			input:    mustParseCIDR("10.0.0.0/8"),
			offset:   100,
			expected: mustParseCIDR("10.0.100.0/24"),
		},
		{
			desc:        "should fail because prefix is not 8 or 16",
			input:       mustParseCIDR("10.43.8.67/28"),
			offset:      100,
			expectedErr: errUnsupportedNetwork,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			actual, err := calculateV4Subnet(tC.input, tC.offset)
			if tC.expectedErr != nil {
				assert.ErrorIs(t, err, tC.expectedErr)
				return
			}
			assertEqualIPNet(t, actual, tC.expected)
		})
	}
}

func assertEqualIPNet(t *testing.T, a, b net.IPNet) {
	assert.True(t, ipNetEqual(&a, &b), "IP networks are not equal: %s != %s", a.String(), b.String())
}

func mustParseCIDR(s string) net.IPNet {
	_, net, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return *net
}

// ipNetEqual compares two net.IPNet instances for equality.
func ipNetEqual(a, b *net.IPNet) bool {
	// Check if the IPs are the same
	if !a.IP.Equal(b.IP) {
		return false
	}
	// Check if the masks are the same
	if len(a.Mask) != len(b.Mask) {
		return false
	}
	for i := range a.Mask {
		if a.Mask[i] != b.Mask[i] {
			return false
		}
	}
	return true
}
