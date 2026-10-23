package mail

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ErrQueueFull: llegaron más correos de los que caben en espera.
var ErrQueueFull = errors.New("mail queue full")

// Queue envía en segundo plano, fuera de la petición, y reintenta con espera
// creciente si el proveedor falla. No registra el contenido del correo.
type Queue struct {
	mailer   Mailer
	log      *slog.Logger
	attempts int
	backoff  time.Duration
	pending  chan Message
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewQueue arranca la cola. attempts son los envíos por correo; backoff, la
// espera antes del segundo (se duplica en cada reintento).
func NewQueue(mailer Mailer, log *slog.Logger, attempts int, backoff time.Duration) *Queue {
	q := &Queue{
		mailer: mailer, log: log, attempts: attempts, backoff: backoff,
		pending: make(chan Message, 100), done: make(chan struct{}),
	}
	q.wg.Add(1)
	go q.run()
	return q
}

// Send deja el correo en la cola y vuelve enseguida.
func (q *Queue) Send(_ context.Context, message Message) error {
	select {
	case q.pending <- message:
		return nil
	default:
		return ErrQueueFull
	}
}

// Close deja de aceptar correos y espera a que salgan los que ya están, hasta
// que ctx termine.
func (q *Queue) Close(ctx context.Context) {
	close(q.done)
	finished := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-ctx.Done():
		q.log.Warn("mail queue closed with emails pending", "pending", len(q.pending))
	}
}

func (q *Queue) run() {
	defer q.wg.Done()
	for {
		select {
		case message := <-q.pending:
			q.deliver(message)
		case <-q.done:
			for {
				select {
				case message := <-q.pending:
					q.deliver(message)
				default:
					return
				}
			}
		}
	}
}

func (q *Queue) deliver(message Message) {
	wait := q.backoff
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := q.mailer.Send(ctx, message)
		cancel()
		if err == nil {
			return
		}
		if attempt >= q.attempts {
			q.log.Error("email not sent", "subject", message.Subject, "attempts", attempt, "error", err)
			return
		}
		q.log.Warn("email failed, retrying", "attempt", attempt, "retry_in", wait, "error", err)
		time.Sleep(wait)
		wait *= 2
	}
}
