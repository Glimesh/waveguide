package whep

type Options func(*WHEPServer)

func WithHTTPS(hostname, cert, key string) Options {
	return func(w *WHEPServer) {
		w.HTTPS = true
		w.HTTPSHostname = hostname
		w.HTTPSCert = cert
		w.HTTPSKey = key
	}
}
