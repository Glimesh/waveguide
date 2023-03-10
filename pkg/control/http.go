package control

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/acme/autocert"
)

// This http server should combine any of the inputs / outputs http endpoints into a singular server

func (ctrl *Control) StartHTTPServer() {
	switch ctrl.HTTPServerType {
	case "acme":
		ctrl.log.Infof("Starting ACME http server on %s:443", ctrl.HTTPSHostname)
		ctrl.log.Fatal(http.Serve(
			autocert.NewListener(ctrl.HTTPSHostname),
			logRequest(ctrl.log, ctrl.httpMux),
		))
	case "https":
		ctrl.log.Infof("Starting https server on %s", ctrl.HTTPAddress)
		ctrl.log.Fatal(httpsServer(
			ctrl.HTTPAddress,
			ctrl.HTTPSCert,
			ctrl.HTTPSKey,
			ctrl.log,
			ctrl.httpMux,
		))
	case "http":
		ctrl.log.Infof("Starting http server on %s", ctrl.HTTPAddress)
		ctrl.log.Fatal(httpServer(
			ctrl.HTTPAddress,
			ctrl.log,
			ctrl.httpMux,
		))
	default:
		ctrl.log.Fatalf("unknown http_server_type server option %s", ctrl.HTTPServerType)
	}
}

func (ctrl *Control) RegisterHandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	ctrl.httpMux.HandleFunc(pattern, handler)
}

func (ctrl *Control) HttpServerUrl() string {
	var protocol string
	var host string
	if ctrl.HTTPServerType == "acme" || ctrl.HTTPServerType == "https" {
		protocol = "https"
		host = ctrl.HTTPSHostname
	} else {
		protocol = "http"
		host = ctrl.HTTPAddress
	}

	return fmt.Sprintf("%s://%s", protocol, host)
}

func httpServer(address string, log logrus.FieldLogger, mux *http.ServeMux) error {
	srv := &http.Server{
		Addr:    address,
		Handler: logRequest(log, mux),
	}
	return srv.ListenAndServe()
}
func httpsServer(address, cert, key string, log logrus.FieldLogger, mux *http.ServeMux) error {
	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}
	srv := &http.Server{
		Addr:         address,
		Handler:      logRequest(log, mux),
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}
	return srv.ListenAndServeTLS(cert, key)
}

func logRequest(log logrus.FieldLogger, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}
