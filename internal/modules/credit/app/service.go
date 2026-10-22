// Package app son los casos de uso de tarjetas y compras pendientes, y los
// puertos que necesitan.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

// StoredCard es una tarjeta y si está archivada.
type StoredCard struct {
	domain.Card
	Archived bool
}

// StoredPurchase es una compra con su estado.
type StoredPurchase struct {
	domain.Purchase
	Status  string
	Deleted bool
}

// PendingPurchase es una compra pendiente con su tarjeta.
type PendingPurchase struct {
	domain.Purchase
	Card domain.Card
}

// Cards guarda tarjetas. Todo método recibe el usuario.
type Cards interface {
	List(ctx context.Context, userID uuid.UUID) ([]domain.Card, error)
	Insert(ctx context.Context, userID uuid.UUID, c domain.Card, at time.Time) (inserted bool, err error)
	// Get devuelve domain.ErrNotFound si no existe o es de otro usuario.
	Get(ctx context.Context, userID, id uuid.UUID) (StoredCard, error)
	Update(ctx context.Context, userID uuid.UUID, c domain.Card) error
	Archive(ctx context.Context, userID, id uuid.UUID) error
	CountPending(ctx context.Context, userID, id uuid.UUID) (int64, error)
}

// Purchases guarda compras. Todo método recibe el usuario.
type Purchases interface {
	ListPending(ctx context.Context, userID uuid.UUID) ([]PendingPurchase, error)
	Insert(ctx context.Context, userID uuid.UUID, p domain.Purchase, at time.Time) (inserted bool, err error)
	// Get devuelve domain.ErrNotFound si no existe o es de otro usuario.
	Get(ctx context.Context, userID, id uuid.UUID) (StoredPurchase, error)
	// Update y Delete solo tocan una pendiente; devuelven domain.ErrNotFound si no la hay.
	Update(ctx context.Context, userID uuid.UUID, p domain.Purchase) error
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

// Categories lee categorías del libro (módulo ledger), también las archivadas.
type Categories interface {
	Find(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]FoundCategory, error)
}

// FoundCategory es una categoría del libro y si está archivada.
type FoundCategory struct {
	domain.Category
	Archived bool
}

// Profile da la moneda principal, si se puede usar otra y "hoy" (módulo profile).
type Profile interface {
	MainCurrency(ctx context.Context, userID uuid.UUID) (string, error)
	// RequireUsable devuelve domain.ErrFxNotConfigured si code no se puede usar todavía.
	RequireUsable(ctx context.Context, userID uuid.UUID, code string) error
	Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error)
}

// Service son los casos de uso de tarjetas y compras.
type Service struct {
	cards      Cards
	purchases  Purchases
	categories Categories
	profile    Profile
	clock      clock.Clock
}

// NewService crea el servicio.
func NewService(cards Cards, purchases Purchases, categories Categories, profile Profile, c clock.Clock) *Service {
	return &Service{cards: cards, purchases: purchases, categories: categories, profile: profile, clock: c}
}

// --- Tarjetas ---

// ListCards devuelve las tarjetas activas con su deuda por moneda.
func (s *Service) ListCards(ctx context.Context, userID uuid.UUID) ([]domain.CardWithDebt, error) {
	cards, err := s.cards.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	pending, err := s.purchases.ListPending(ctx, userID)
	if err != nil {
		return nil, err
	}
	main, err := s.profile.MainCurrency(ctx, userID)
	if err != nil {
		return nil, err
	}
	return domain.CardsWithDebt(cards, purchasesOf(pending), main), nil
}

// CardInput es Nueva/Editar tarjeta. Al editar, lo que no viene no cambia.
type CardInput struct {
	Name     *string
	Currency *string
}

// CreateCard guarda una tarjeta. Sin tipo de cambio, solo en la moneda principal.
func (s *Service) CreateCard(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in CardInput) (domain.Card, bool, error) {
	var problems domain.ValidationError
	card := domain.Card{ID: ids.New()}
	name, code := "", ""
	if in.Name != nil {
		name = *in.Name
	}
	if in.Currency != nil {
		code = *in.Currency
	}
	card.Name = domain.ValidateCardName(name, &problems)
	domain.ValidateCurrency(code, &problems)
	if err := problems.OrNil(); err != nil {
		return domain.Card{}, false, err
	}
	card.Currency = code
	if id != nil {
		card.ID = *id
	}
	if err := s.profile.RequireUsable(ctx, userID, card.Currency); err != nil {
		return domain.Card{}, false, err
	}
	inserted, err := s.cards.Insert(ctx, userID, card, s.clock.Now())
	if err != nil || inserted {
		return card, inserted, err
	}
	existing, err := s.cards.Get(ctx, userID, card.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Card{}, false, domain.ErrIDTaken
	}
	if err != nil {
		return domain.Card{}, false, err
	}
	if existing.Archived || existing.Card != card {
		return domain.Card{}, false, domain.ErrIDTaken
	}
	return existing.Card, false, nil
}

// UpdateCard cambia nombre o moneda. La moneda nueva vale para las compras
// nuevas; las que ya existen conservan la suya.
func (s *Service) UpdateCard(ctx context.Context, userID, id uuid.UUID, in CardInput) (domain.Card, error) {
	current, err := s.cards.Get(ctx, userID, id)
	if err != nil {
		return domain.Card{}, err
	}
	if current.Archived {
		return domain.Card{}, domain.ErrNotFound
	}
	next := current.Card
	var problems domain.ValidationError
	if in.Name != nil {
		next.Name = domain.ValidateCardName(*in.Name, &problems)
	}
	if in.Currency != nil {
		domain.ValidateCurrency(*in.Currency, &problems)
		next.Currency = *in.Currency
	}
	if err := problems.OrNil(); err != nil {
		return domain.Card{}, err
	}
	if next.Currency != current.Currency {
		if err := s.profile.RequireUsable(ctx, userID, next.Currency); err != nil {
			return domain.Card{}, err
		}
	}
	if err := s.cards.Update(ctx, userID, next); err != nil {
		return domain.Card{}, err
	}
	return next, nil
}

// ArchiveCard archiva una tarjeta que no debe nada. Archivar una ya archivada no
// es un error.
func (s *Service) ArchiveCard(ctx context.Context, userID, id uuid.UUID) error {
	current, err := s.cards.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if current.Archived {
		return nil
	}
	pending, err := s.cards.CountPending(ctx, userID, id)
	if err != nil {
		return err
	}
	if pending > 0 {
		return domain.ErrCardHasPending
	}
	return s.cards.Archive(ctx, userID, id)
}

// --- Compras ---

// PurchaseView es una compra con su tarjeta y su categoría, como la muestra la app.
type PurchaseView struct {
	domain.Purchase
	Status   string
	Card     domain.Card
	Category domain.Category
}

// ListPending devuelve las compras pendientes, de la fecha límite más próxima a
// la más lejana.
func (s *Service) ListPending(ctx context.Context, userID uuid.UUID) ([]PurchaseView, error) {
	pending, err := s.purchases.ListPending(ctx, userID)
	if err != nil {
		return nil, err
	}
	categoryIDs := make([]uuid.UUID, 0, len(pending))
	for _, p := range pending {
		categoryIDs = append(categoryIDs, p.CategoryID)
	}
	categories, err := s.categories.Find(ctx, userID, categoryIDs)
	if err != nil {
		return nil, err
	}
	out := make([]PurchaseView, 0, len(pending))
	for _, p := range pending {
		out = append(out, PurchaseView{
			Purchase: p.Purchase, Status: domain.StatusPending, Card: p.Card, Category: categories[p.CategoryID].Category,
		})
	}
	return out, nil
}

// PurchaseInput es la compra con tarjeta de Nuevo movimiento, y Editar compra
// (donde lo que no viene no cambia).
type PurchaseInput struct {
	CardID      *uuid.UUID
	CategoryID  *uuid.UUID
	Amount      *string
	Description *string
	DueDate     *string
}

// CreatePurchase guarda una compra pendiente. La fecha de compra es hoy y la
// moneda, la de la tarjeta. Un reintento con el mismo id y contenido devuelve
// la existente.
func (s *Service) CreatePurchase(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in PurchaseInput) (PurchaseView, bool, error) {
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return PurchaseView{}, false, err
	}
	var problems domain.ValidationError
	p := domain.Purchase{ID: ids.New(), PurchaseDate: today}
	p.Amount = domain.ValidateAmount(in.Amount, &problems)
	if in.CardID == nil {
		problems.Add("card_id", domain.CodeCardRequired)
	}
	if in.CategoryID == nil {
		problems.Add("category_id", domain.CodeCategoryRequired)
	}
	description := ""
	if in.Description != nil {
		description = *in.Description
	}
	p.Description = domain.ValidateDescription(description, &problems)
	p.DueDate = domain.ValidateDueDate(in.DueDate, today, &problems)
	if err := problems.OrNil(); err != nil {
		return PurchaseView{}, false, err
	}
	if id != nil {
		p.ID = *id
	}
	card, err := s.usableCard(ctx, userID, *in.CardID)
	if err != nil {
		return PurchaseView{}, false, err
	}
	if err := s.profile.RequireUsable(ctx, userID, card.Currency); err != nil {
		return PurchaseView{}, false, err
	}
	category, err := s.usableCategory(ctx, userID, *in.CategoryID)
	if err != nil {
		return PurchaseView{}, false, err
	}
	p.CardID, p.CategoryID, p.Currency = card.ID, category.ID, card.Currency

	inserted, err := s.purchases.Insert(ctx, userID, p, s.clock.Now())
	if err != nil {
		return PurchaseView{}, false, err
	}
	if !inserted {
		existing, err := s.purchases.Get(ctx, userID, p.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return PurchaseView{}, false, domain.ErrIDTaken
		}
		if err != nil {
			return PurchaseView{}, false, err
		}
		if existing.Deleted || !samePurchase(existing.Purchase, p) {
			return PurchaseView{}, false, domain.ErrIDTaken
		}
		p = existing.Purchase
	}
	return PurchaseView{Purchase: p, Status: domain.StatusPending, Card: card, Category: category}, inserted, nil
}

// samePurchase compara un reintento; la fecha de compra la pone el API y no cuenta.
func samePurchase(a, b domain.Purchase) bool {
	return a.CardID == b.CardID && a.CategoryID == b.CategoryID && a.Description == b.Description &&
		a.Amount.Equal(b.Amount) && a.DueDate == b.DueDate
}

// UpdatePurchase cambia una compra pendiente. Movida a otra tarjeta toma su
// moneda; si no, conserva la suya. La fecha límite puede quedar en el pasado
// (para corregir una vencida), pero no antes de la fecha de compra.
func (s *Service) UpdatePurchase(ctx context.Context, userID, id uuid.UUID, in PurchaseInput) (PurchaseView, error) {
	current, err := s.pendingPurchase(ctx, userID, id)
	if err != nil {
		return PurchaseView{}, err
	}
	next := current.Purchase
	var problems domain.ValidationError
	if in.Amount != nil {
		next.Amount = domain.ValidateAmount(in.Amount, &problems)
	}
	if in.Description != nil {
		next.Description = domain.ValidateDescription(*in.Description, &problems)
	}
	if in.DueDate != nil {
		next.DueDate = domain.ValidateDueDate(in.DueDate, current.PurchaseDate, &problems)
	}
	if err := problems.OrNil(); err != nil {
		return PurchaseView{}, err
	}

	var card domain.Card
	if in.CardID != nil && *in.CardID != current.CardID {
		if card, err = s.usableCard(ctx, userID, *in.CardID); err != nil {
			return PurchaseView{}, err
		}
		if err := s.profile.RequireUsable(ctx, userID, card.Currency); err != nil {
			return PurchaseView{}, err
		}
		next.CardID, next.Currency = card.ID, card.Currency
	} else {
		stored, err := s.cards.Get(ctx, userID, current.CardID)
		if err != nil {
			return PurchaseView{}, err
		}
		card = stored.Card
	}
	var category domain.Category
	if in.CategoryID != nil && *in.CategoryID != current.CategoryID {
		if category, err = s.usableCategory(ctx, userID, *in.CategoryID); err != nil {
			return PurchaseView{}, err
		}
		next.CategoryID = category.ID
	} else {
		found, err := s.categories.Find(ctx, userID, []uuid.UUID{current.CategoryID})
		if err != nil {
			return PurchaseView{}, err
		}
		category = found[current.CategoryID].Category
	}

	if err := s.purchases.Update(ctx, userID, next); err != nil {
		return PurchaseView{}, err
	}
	return PurchaseView{Purchase: next, Status: domain.StatusPending, Card: card, Category: category}, nil
}

// DeletePurchase elimina una compra pendiente; solo cambia la deuda de su
// tarjeta. Eliminar una que ya no existe no es un error.
func (s *Service) DeletePurchase(ctx context.Context, userID, id uuid.UUID) error {
	current, err := s.purchases.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if current.Deleted {
		return nil
	}
	if current.Status == domain.StatusPaid {
		return domain.ErrPurchaseAlreadyPaid
	}
	return s.purchases.Delete(ctx, userID, id)
}

// Summary es el aviso de tarjetas de Inicio; ok es false si no hay pendientes.
func (s *Service) Summary(ctx context.Context, userID uuid.UUID) (domain.Summary, bool, error) {
	pending, err := s.purchases.ListPending(ctx, userID)
	if err != nil {
		return domain.Summary{}, false, err
	}
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return domain.Summary{}, false, err
	}
	summary, ok := domain.Summarize(purchasesOf(pending), today)
	return summary, ok, nil
}

func (s *Service) pendingPurchase(ctx context.Context, userID, id uuid.UUID) (StoredPurchase, error) {
	current, err := s.purchases.Get(ctx, userID, id)
	if err != nil {
		return StoredPurchase{}, err
	}
	if current.Deleted {
		return StoredPurchase{}, domain.ErrNotFound
	}
	if current.Status == domain.StatusPaid {
		return StoredPurchase{}, domain.ErrPurchaseAlreadyPaid
	}
	return current, nil
}

func (s *Service) usableCard(ctx context.Context, userID, id uuid.UUID) (domain.Card, error) {
	card, err := s.cards.Get(ctx, userID, id)
	if err != nil {
		return domain.Card{}, err
	}
	if card.Archived {
		return domain.Card{}, domain.ErrCardArchived
	}
	return card.Card, nil
}

// usableCategory: una categoría propia, activa y de egreso.
func (s *Service) usableCategory(ctx context.Context, userID, id uuid.UUID) (domain.Category, error) {
	found, err := s.categories.Find(ctx, userID, []uuid.UUID{id})
	if err != nil {
		return domain.Category{}, err
	}
	category, ok := found[id]
	switch {
	case !ok:
		return domain.Category{}, domain.ErrNotFound
	case category.Archived:
		return domain.Category{}, domain.ErrCategoryArchived
	case category.Kind != "expense":
		return domain.Category{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "category_id", Code: domain.CodeCategoryKindMismatch}}}
	}
	return category.Category, nil
}

func purchasesOf(pending []PendingPurchase) []domain.Purchase {
	out := make([]domain.Purchase, 0, len(pending))
	for _, p := range pending {
		out = append(out, p.Purchase)
	}
	return out
}
