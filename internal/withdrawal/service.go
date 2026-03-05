package withdrawal

import "context"

type Repository interface {
	CreateWithdrawal(ctx context.Context, req CreateRequest, amount Amount) (CreateResponse, bool, error)
	GetWithdrawal(ctx context.Context, id string) (Withdrawal, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) CreateWithdrawal(ctx context.Context, req CreateRequest) (CreateResponse, bool, error) {
	amount, err := req.Validate()
	if err != nil {
		return CreateResponse{}, false, err
	}
	return s.repository.CreateWithdrawal(ctx, req, amount)
}

func (s *Service) GetWithdrawal(ctx context.Context, id string) (Withdrawal, error) {
	if id == "" {
		return Withdrawal{}, ErrNotFound
	}
	return s.repository.GetWithdrawal(ctx, id)
}
