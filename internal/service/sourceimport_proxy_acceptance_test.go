//go:build sourceproxyacceptance

package service

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

// Opt-in only: the ordinary unit/internal suites never contact external sites.
func TestSourceImportNativeProxyExample(t *testing.T) {
	destination, _ := url.Parse("https://example.com")
	proxy, err := sourceConfiguredProxy(destination)
	if err != nil {
		t.Fatal(err)
	}
	if proxy == nil {
		t.Skip("native proxy acceptance requires an explicitly configured fixed proxy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", destination.Hostname())
	if err != nil || len(addresses) == 0 {
		t.Fatal("native acceptance could not read the current OS DNS answers")
	}
	allFake := true
	for _, address := range addresses {
		allFake = allFake && netip.MustParsePrefix("198.18.0.0/15").Contains(address.Unmap())
	}
	t.Logf("OS DNS answers all use the proxy fake-IP range: %t", allFake)
	data, _, _, err := NewImportService().sourceFetchURL(ctx, destination.String())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("Example Domain")) {
		t.Fatal("native proxy fetch did not return the expected public example page")
	}
	t.Logf("fetched and verified %d bytes from the public example page", len(data))
}
