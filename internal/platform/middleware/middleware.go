// Package middleware tiene lo que corre en todas las peticiones: request id,
// recuperación de panics, log de acceso, CORS, límite por IP y tamaño del cuerpo.
package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
)

// RequestIDHeader viaja en la petición y en la respuesta.
const RequestIDHeader = "X-Request-Id"

// Solo se respeta un id del cliente que no pueda ensuciar los logs.
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID toma el X-Request-Id del cliente si es seguro, o genera uno, y deja
// en el contexto un logger que lo lleva en cada línea.
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !safeRequestID.MatchString(id) {
			id = ids.New().String()
		}
		c.Header(RequestIDHeader, id)
		ctx := logger.WithContext(c.Request.Context(), base.With("request_id", id))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// Recover convierte un panic en un 500 con el formato de error. Un handler
// roto no tumba el proceso ni deja sin respuesta a los demás.
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.From(c.Request.Context()).Error("panic recovered",
					"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				apierror.Respond(c, apierror.ErrInternal)
			}
		}()
		c.Next()
	}
}

// AccessLog registra método, ruta, estado y duración. Nunca cabeceras ni cuerpos:
// llevan tokens, contraseñas y datos financieros. El health check no se registra:
// Docker lo llama cada 30 segundos.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if strings.HasSuffix(path, "/health") || strings.HasSuffix(path, "/health/ready") {
			return
		}
		if path == "" {
			path = "unmatched"
		}
		logger.From(c.Request.Context()).Info("request",
			"method", c.Request.Method,
			"route", path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

// CORS deja pasar solo a los orígenes configurados. Las apps nativas no envían
// Origin; esto afecta solo a navegadores (la web).
func CORS(allowed []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && slices.Contains(allowed, origin) {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			// La web manda la cookie de sesión (plan 17).
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", RequestIDHeader+", Retry-After")
			if c.Request.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Session-Transport, "+RequestIDHeader)
				h.Set("Access-Control-Max-Age", "600")
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}

// BodyLimit corta los cuerpos más grandes que limit; al leerlos, el handler
// recibe un *http.MaxBytesError que apierror traduce a 413. larger da un límite
// propio a rutas puntuales (la subida de archivos), por su ruta de Gin.
func BodyLimit(limit int64, larger map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := limit
		if routeLimit, ok := larger[c.FullPath()]; ok {
			limit = routeLimit
		}
		if c.Request.ContentLength > limit {
			apierror.Respond(c, apierror.ErrPayloadTooLarge)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// RateLimiter limita las peticiones por IP en memoria. Basta con una réplica;
// con varias, el contador tendría que ser compartido.
type RateLimiter struct {
	perMinute int
	mu        sync.Mutex
	visitors  map[string]*visitor
	now       func() time.Time
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter permite perMinute peticiones por minuto por IP, con ráfagas del
// mismo tamaño.
func NewRateLimiter(perMinute int) *RateLimiter {
	return &RateLimiter{perMinute: perMinute, visitors: map[string]*visitor{}, now: time.Now}
}

// Middleware devuelve el handler de Gin. La IP es c.ClientIP(), que solo cree a
// X-Forwarded-For si la petición llega desde un proxy de confianza (nginx).
func (l *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		limiter := l.limiterFor(c.ClientIP())
		reservation := limiter.Reserve()
		if delay := reservation.Delay(); delay > 0 {
			reservation.Cancel()
			c.Header("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
			apierror.Respond(c, apierror.ErrTooManyRequests)
			return
		}
		c.Next()
	}
}

func (l *RateLimiter) limiterFor(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// Limpieza perezosa: quien no aparece hace 5 minutos ya tiene su cupo lleno.
	if len(l.visitors) > 10_000 {
		for key, v := range l.visitors {
			if now.Sub(v.lastSeen) > 5*time.Minute {
				delete(l.visitors, key)
			}
		}
	}
	v, ok := l.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.perMinute)), l.perMinute)}
		l.visitors[ip] = v
	}
	v.lastSeen = now
	return v.limiter
}
