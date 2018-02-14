package centiment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/pprof"
	"time"
	"unicode/utf8"

	"github.com/go-kit/kit/log"
	"github.com/gorilla/mux"
	"github.com/gosimple/slug"
	"github.com/pkg/errors"
)

// Env contains the application dependencies.
// TODO(matt): Make this type Server
type Env struct {
	DB       DB
	Hostname string
	Logger   log.Logger
}

// Endpoint represents a application server endpoint. It bundles a
// error-returning handler and injects our application dependencies.
type Endpoint struct {
	Env     *Env
	Handler func(*Env, http.ResponseWriter, *http.Request) error
}

// HTTPError represents a HTTP error.
type HTTPError struct {
	Code int   `json:"code"`
	Err  error `json:"error"`
}

func (he HTTPError) Error() string {
	return fmt.Sprintf("code=%d err=%s", he.Code, he.Err)
}

// JSON formats the current HTTPError as JSON.
func (he HTTPError) JSON() ([]byte, error) {
	return json.Marshal(he)
}

// ServeHTTP implements http.Handler for an Endpoint.
func (ep *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := ep.Handler(ep.Env, w, r); err != nil {
		switch e := err.(type) {
		case HTTPError:
			w.Header().Set("Content-Type", "application/json")
			b, err := e.JSON()
			if err != nil {
				serverError(w)
			}
			w.Write(b)
		default:
			ep.Env.Logger.Log("err", err, "msg", "serverError")
			serverError(w)
		}

		return
	}
}

// AddIndexEndpoints adds the entrypoint/index handlers to the given router.
func AddIndexEndpoints(r *mux.Router, env *Env) *mux.Router {
	h := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s\n", env.Hostname)
	}
	r.HandleFunc("/", h)

	return r
}

// AddSentimentEndpoints adds the sentiment endpoints to the given router, and
// returns an instance of the Subrouter.
func AddSentimentEndpoints(r *mux.Router, env *Env) *mux.Router {
	s := r.PathPrefix("/sentiments").Subrouter()
	s.Handle("/{topicSlug}", &Endpoint{Env: env, Handler: sentimentHandler})
	s.Handle("/{topicSlug}", &Endpoint{Env: env, Handler: sentimentHandler}).
		Queries("count", "{count}")
	s.Handle("/{topicSlug}", &Endpoint{Env: env, Handler: sentimentHandler}).
		Queries("before", "{before}")
	s.Handle("/{topicSlug}", &Endpoint{Env: env, Handler: sentimentHandler}).
		Queries("after", "{after}")
	s.Handle("/{topicSlug}", &Endpoint{Env: env, Handler: sentimentHandler}).
		Queries("id", "{id}")

	return s
}

// AddMetricEndpoints adds the metric/debugging endpoints to the given router, and
// returns an instance of the Subrouter.
func AddMetricEndpoints(r *mux.Router, env *Env) *mux.Router {
	m := r.PathPrefix("/metrics").Subrouter()
	m.Handle("/{profile}", &Endpoint{Env: env, Handler: metricsHandler})

	return m
}

// AddHealthCheckEndpoints adds the health check endpoints to the given router, and
// returns an instance of the Subrouter.
func AddHealthCheckEndpoints(r *mux.Router, env *Env) *mux.Router {
	// App Engine health checks.
	h := r.PathPrefix("/health").Subrouter()
	h.Handle("/{type}", &Endpoint{Env: env, Handler: healthCheckHandler})

	return h
}

// LogRequest logs each HTTP request, using the given logger.
func LogRequest(logger log.Logger) func(http.Handler) http.Handler {
	fn := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Log(
				"method", r.Method,
				"host", r.Host,
				"url", r.URL.String(),
				"ip", r.RemoteAddr,
				"forwarded-ip", r.Header.Get("X-Forwarded-For"),
				"duration", time.Since(start),
			)
		})
	}

	return fn
}

func sentimentHandler(env *Env, w http.ResponseWriter, r *http.Request) error {
	vars := mux.Vars(r)

	topicSlug, ok := vars["topicSlug"]
	if !ok {
		return errors.New("no topic provided to route")
	}

	if !slug.IsSlug(topicSlug) {
		return errors.Errorf("not a valid topic format: %s is not slugified", topicSlug)
	}

	if count := utf8.RuneCountInString(topicSlug); count > 100 {
		return errors.Errorf("topic too long: 100 rune limit (got %d)", count)
	}

	res, err := env.DB.GetSentimentsBySlug(
		context.Background(),
		topicSlug,
		10,
	)
	if err != nil {
		return err
	}

	b, err := json.Marshal(res)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(b)

	return nil
}

func metricsHandler(env *Env, w http.ResponseWriter, r *http.Request) error {
	vars := mux.Vars(r)
	profile := vars["profile"]
	data := pprof.Lookup(profile)

	if data == nil {
		// TODO(matt): Return a &HTTPError{err, code}
		return errors.Errorf("profile %q does not exist", profile)
	}

	return data.WriteTo(w, 1)
}

func healthCheckHandler(env *Env, w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
	return nil
}

// RunServer runs the configured server.
// TODO(matt): Create a NewServer constructor -> call srv.Run()
func RunServer(srv *http.Server) func() error {
	return func() error {
		return srv.ListenAndServe()
	}
}

func shutdownServer(ctx context.Context, logger log.Logger, srv *http.Server) func(error) {
	return func(error) {
		if err := srv.Shutdown(ctx); err != nil {
			logger.Log("err", err)
			return
		}

		logger.Log("status", "returning from shutdownServer")
	}
}

func serverError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `{"error":"application_error"}`)
}
