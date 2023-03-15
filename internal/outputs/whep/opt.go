package whep

type Options func(*Server)

func WithHTTPS(hostname, cert, key string) Options {
	return func(w *Server) {
		w.HTTPS = true
		w.HTTPSHostname = hostname
		w.HTTPSCert = cert
		w.HTTPSKey = key
	}
}
