package dnsnameresolver

import (
	"context"
	"strings"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/log"
	"github.com/miekg/dns"
	// "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"

	"k8s.io/client-go/tools/cache"
)

type DNSNameResolver struct {
	Next plugin.Handler

	// networkClient versioned.Interface
	dnsNameResolverInformer cache.SharedIndexInformer
}

type responseWriter struct {
	dns.ResponseWriter
	msg *dns.Msg
}

func (w *responseWriter) WriteMsg(msg *dns.Msg) error {
	w.msg = msg
	return w.ResponseWriter.WriteMsg(msg)
}

func (r DNSNameResolver) ServeDNS(ctx context.Context, w dns.ResponseWriter, m *dns.Msg) (int, error) {
	var queryName string
	if len(m.Question) > 0 {
		queryName = strings.TrimSuffix(m.Question[0].Name, ".")
	}

	rw := &responseWriter{ResponseWriter: w}
	rcode, err := r.Next.ServeDNS(ctx, rw, m)

	if queryName != "" {
		r.logDNSResult(queryName, rw.msg, rcode)
	}

	return rcode, err
}

func (r DNSNameResolver) logDNSResult(queryName string, msg *dns.Msg, rcode int) {
	var ips []string
	if msg != nil {
		ips = r.extractIPs(msg)
	}

	log.Infof("DNS Query: domain=%s, ips=%s, rcode=%s",
		queryName,
		strings.Join(ips, ","),
		dns.RcodeToString[rcode])
}

func (r DNSNameResolver) extractIPs(msg *dns.Msg) []string {
	var ips []string
	for _, rr := range msg.Answer {
		switch record := rr.(type) {
		case *dns.A:
			ips = append(ips, record.A.String())
		case *dns.AAAA:
			ips = append(ips, record.AAAA.String())
		}
	}
	return ips
}

func (r DNSNameResolver) Name() string {
	return "dnsnameresolver"
}
