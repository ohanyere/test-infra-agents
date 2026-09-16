package identity

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const maxNameLength = 200

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var unsupportedSlugCharacters = regexp.MustCompile(`[^a-z0-9]+`)

func ValidateOrganization(input OrganizationInput) (OrganizationInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.TrimSpace(input.Slug)
	if err := validateName("organization name", input.Name); err != nil {
		return input, err
	}
	if err := ValidateSlug(input.Slug); err != nil {
		return input, err
	}
	return input, nil
}

func ValidateWorkspace(input WorkspaceInput) (WorkspaceInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.TrimSpace(input.Slug)
	if err := validateName("workspace name", input.Name); err != nil {
		return input, err
	}
	if err := ValidateSlug(input.Slug); err != nil {
		return input, err
	}
	return input, nil
}

func ValidateUser(input UserInput) (UserInput, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.FullName = strings.TrimSpace(input.FullName)
	if input.Email == "" {
		return input, fmt.Errorf("%w: email is required", ErrValidation)
	}
	if len(input.Email) > 320 {
		return input, fmt.Errorf("%w: email is too long", ErrValidation)
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || !strings.Contains(input.Email, "@") {
		return input, fmt.Errorf("%w: enter a valid email address", ErrValidation)
	}
	if err := validateName("full name", input.FullName); err != nil {
		return input, err
	}
	return input, nil
}

func ValidateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("%w: slug is required", ErrValidation)
	}
	if len(slug) > 100 {
		return fmt.Errorf("%w: slug must be at most 100 characters", ErrValidation)
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("%w: slug must contain lowercase letters, numbers, and single hyphen separators", ErrValidation)
	}
	return nil
}

func GenerateSlug(name string) (string, error) {
	slug := strings.Trim(unsupportedSlugCharacters.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
	if slug == "" {
		return "", fmt.Errorf("%w: a slug could not be generated from the name", ErrValidation)
	}
	if len(slug) > 100 {
		slug = strings.TrimRight(slug[:100], "-")
	}
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	return slug, nil
}

func ValidateRole(role string) error {
	switch role {
	case "owner", "admin", "member", "viewer":
		return nil
	}
	return fmt.Errorf("%w: role must be owner, admin, member, or viewer", ErrValidation)
}

func ValidateUUID(value string) error {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil || !id.Valid {
		return fmt.Errorf("%w: invalid UUID", ErrValidation)
	}
	return nil
}

func validateName(label, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrValidation, label)
	}
	if len(value) > maxNameLength {
		return fmt.Errorf("%w: %s must be at most %d characters", ErrValidation, label, maxNameLength)
	}
	return nil
}
