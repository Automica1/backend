// internal/services/user_service.go
package services

import (
	"context"
	"log"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"
)

type UserService interface {
	RegisterUser(ctx context.Context, req *models.RegisterUserRequest) (*models.RegisterUserResponse, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetOrCreateUser(ctx context.Context, email string) (*models.User, error)
}

type userService struct {
	userRepo    repository.UserRepository
	creditsRepo repository.CreditsRepository
}

func NewUserService(userRepo repository.UserRepository, creditsRepo repository.CreditsRepository) UserService {
	return &userService{
		userRepo:    userRepo,
		creditsRepo: creditsRepo,
	}
}

func (s *userService) RegisterUser(ctx context.Context, req *models.RegisterUserRequest) (*models.RegisterUserResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	// Check if user already exists
	_, err := s.userRepo.GetByUserID(ctx, req.UserID)
	if err == nil {
		// User exists, return error
		return nil, apperrors.NewUserAlreadyExistsError()
	}
	
	// Check if the error is something other than "user not found"
	if !apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
		// Some other error occurred (database error, etc.)
		return nil, err
	}

	// User doesn't exist, proceed with creation
	user := &models.User{
		UserID: req.UserID,
		Email:  req.Email,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	// Create initial credits (10 credits)
	const initialCredits = 10
	credits := &models.Credits{
		UserID:  req.UserID,
		Credits: initialCredits,
	}

	if err := s.creditsRepo.Create(ctx, credits); err != nil {
		// Rollback user creation if credits creation fails
		if deleteErr := s.userRepo.Delete(ctx, req.UserID); deleteErr != nil {
			log.Printf("Failed to rollback user creation: %v", deleteErr)
		}
		return nil, err
	}

	return &models.RegisterUserResponse{
		Message: "User registered successfully",
		User:    *user,
		Credits: initialCredits,
	}, nil
}

func (s *userService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return s.userRepo.GetByEmail(ctx, email)
}

// GetOrCreateUser gets a user by email, or creates one if it doesn't exist
func (s *userService) GetOrCreateUser(ctx context.Context, email string) (*models.User, error) {
	// First, try to get the user by email
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err == nil {
		// User exists, return it
		return user, nil
	}

	// Check if the error is "user not found"
	if !apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
		// Some other error occurred (database error, etc.)
		return nil, err
	}

	// User doesn't exist, create a new one
	// Generate a UserID from email (you might want to use a UUID instead)
	userID := email // Or use uuid.New().String() for a proper UUID

	newUser := &models.User{
		UserID: userID,
		Email:  email,
	}

	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, err
	}

	// Create initial credits for the new user
	const initialCredits = 10
	credits := &models.Credits{
		UserID:  userID,
		Credits: initialCredits,
	}

	if err := s.creditsRepo.Create(ctx, credits); err != nil {
		// Rollback user creation if credits creation fails
		if deleteErr := s.userRepo.Delete(ctx, userID); deleteErr != nil {
			log.Printf("Failed to rollback user creation during GetOrCreateUser: %v", deleteErr)
		}
		return nil, err
	}

	return newUser, nil
}