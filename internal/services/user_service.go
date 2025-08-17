// internal/services/user_service.go
package services

import (
	"context"
	"log"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"
)

type UserService interface {
	RegisterUser(ctx context.Context, req *models.RegisterUserRequest) (*models.RegisterUserResponse, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetOrCreateUser(ctx context.Context, email string) (*models.User, error)
	// Add these new admin methods
	GetAllUsers(ctx context.Context) (*models.AdminUserListResponse, error)
	GetUserByID(ctx context.Context, userID string) (*models.AdminUserDetailResponse, error)
	GetUserStats(ctx context.Context) (*models.UserStatsResponse, error)
	GetUserActivity(ctx context.Context, userID string) (*models.UserActivityResponse, error)
	GetUserCredits(ctx context.Context, userID string) (*models.UserCreditsResponse, error)
}

type userService struct {
	userRepo     repository.UserRepository
	creditsRepo  repository.CreditsRepository
	activityRepo repository.ActivityRepository // Add this
}

func NewUserService(userRepo repository.UserRepository, creditsRepo repository.CreditsRepository, activityRepo repository.ActivityRepository) UserService {
	return &userService{
		userRepo:     userRepo,
		creditsRepo:  creditsRepo,
		activityRepo: activityRepo,
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
	now := time.Now()
	user := &models.User{
		UserID:    req.UserID,
		Email:     req.Email,
		CreatedAt: now,
		UpdatedAt: now,
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

	now := time.Now()
	newUser := &models.User{
		UserID:    userID,
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
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

// Admin methods

func (s *userService) GetAllUsers(ctx context.Context) (*models.AdminUserListResponse, error) {
	// Get users with their credits using aggregation
	adminUsers, err := s.creditsRepo.GetAllWithUsers(ctx)
	if err != nil {
		return nil, err
	}

	return &models.AdminUserListResponse{
		Message: "Users retrieved successfully",
		Users:   adminUsers,
		Total:   len(adminUsers),
	}, nil
}

func (s *userService) GetUserByID(ctx context.Context, userID string) (*models.AdminUserDetailResponse, error) {
	// Get user details
	user, err := s.userRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get user's credits
	credits, err := s.creditsRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Create admin user response with all user fields including timestamps
	adminUser := models.AdminUser{
		ID:        user.ID,
		UserID:    user.UserID,
		Email:     user.Email,
		Credits:   credits.Credits,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}

	return &models.AdminUserDetailResponse{
		Message: "User details retrieved successfully",
		User:    adminUser,
	}, nil
}

func (s *userService) GetUserStats(ctx context.Context) (*models.UserStatsResponse, error) {
	// Get total users count
	totalUsers, err := s.userRepo.GetTotalCount(ctx)
	if err != nil {
		return nil, err
	}

	// Get total credits
	totalCredits, err := s.creditsRepo.GetTotalCredits(ctx)
	if err != nil {
		return nil, err
	}

	// Calculate average credits
	var avgCredits float64
	if totalUsers > 0 {
		avgCredits = float64(totalCredits) / float64(totalUsers)
	}

	return &models.UserStatsResponse{
		Message:      "User statistics retrieved successfully",
		TotalUsers:   totalUsers,
		TotalCredits: totalCredits,
		AvgCredits:   avgCredits,
	}, nil
}

func (s *userService) GetUserActivity(ctx context.Context, userID string) (*models.UserActivityResponse, error) {
	// First verify user exists
	_, err := s.userRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get user activities
	activities, err := s.activityRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &models.UserActivityResponse{
		Message:    "User activity retrieved successfully",
		UserID:     userID,
		Activities: activities,
	}, nil
}

func (s *userService) GetUserCredits(ctx context.Context, userID string) (*models.UserCreditsResponse, error) {
	// First verify user exists
	_, err := s.userRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get user's credits
	credits, err := s.creditsRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &models.UserCreditsResponse{
		Message: "User credits retrieved successfully",
		UserID:  userID,
		Credits: credits.Credits,
	}, nil
}