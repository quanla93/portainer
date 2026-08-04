package auth

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	httperrors "github.com/portainer/portainer/api/http/errors"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"

	"github.com/rs/zerolog/log"
)

type oauthPayload struct {
	// OAuth code returned from OAuth Provided
	Code string
}

func (payload *oauthPayload) Validate(r *http.Request) error {
	if len(payload.Code) == 0 {
		return errors.New("Invalid OAuth authorization code")
	}

	return nil
}

func (handler *Handler) authenticateOAuth(ctx context.Context, code string, settings *portainer.OAuthSettings) (string, map[string]any, error) {
	if code == "" {
		return "", nil, errors.New("Invalid OAuth authorization code")
	}

	if settings == nil {
		return "", nil, errors.New("Invalid OAuth configuration")
	}

	username, claims, err := handler.OAuthService.Authenticate(ctx, code, settings)
	if err != nil {
		return "", nil, err
	}

	return username, claims, nil
}

// @id ValidateOAuth
// @summary Authenticate with OAuth
// @description **Access policy**: public
// @tags auth
// @accept json
// @produce json
// @param body body oauthPayload true "OAuth Credentials used for authentication"
// @success 200 {object} authenticateResponse "Success"
// @failure 400 "Invalid request"
// @failure 422 "Invalid Credentials"
// @failure 500 "Server error"
// @router /auth/oauth/validate [post]
func (handler *Handler) validateOAuth(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload oauthPayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}

	var settings *portainer.Settings
	if err := handler.DataStore.ViewTx(func(tx dataservices.DataStoreTx) error {
		var err error
		settings, err = tx.Settings().Settings()
		return err
	}); err != nil {
		return httperror.InternalServerError("Unable to retrieve settings from the database", err)
	}

	if settings.AuthenticationMethod != portainer.AuthenticationOAuth {
		return httperror.Forbidden("OAuth authentication is not enabled", errors.New("OAuth authentication is not enabled"))
	}

	username, claims, err := handler.authenticateOAuth(r.Context(), payload.Code, &settings.OAuthSettings)
	if err != nil {
		log.Debug().Err(err).Msg("OAuth authentication error")

		return httperror.InternalServerError("Unable to authenticate through OAuth", httperrors.ErrUnauthorized)
	}

	user, err := handler.DataStore.User().UserByUsername(username)
	if err != nil && !handler.DataStore.IsErrObjectNotFound(err) {
		return httperror.InternalServerError("Unable to retrieve a user with the specified username from the database", err)
	}

	if user == nil && !settings.OAuthSettings.OAuthAutoCreateUsers {
		return httperror.Forbidden("Account not created beforehand in Portainer and automatic user provisioning not enabled", httperrors.ErrUnauthorized)
	}

	if user == nil {
		user = &portainer.User{
			Username: username,
			Role:     portainer.StandardUserRole,
		}

		err = handler.DataStore.User().Create(user)
		if err != nil {
			return httperror.InternalServerError("Unable to persist user inside the database", err)
		}

		if settings.OAuthSettings.DefaultTeamID != 0 {
			membership := &portainer.TeamMembership{
				UserID: user.ID,
				TeamID: settings.OAuthSettings.DefaultTeamID,
				Role:   portainer.TeamMember,
			}

			err = handler.DataStore.TeamMembership().Create(membership)
			if err != nil {
				return httperror.InternalServerError("Unable to persist team membership inside the database", err)
			}
		}
	}

	if err := handler.syncUserTeamsWithOAuthClaims(user, claims, &settings.OAuthSettings); err != nil {
		log.Warn().Err(err).Msg("unable to automatically sync user teams with oauth")
	}

	return handler.writeToken(w, r, user, false, settings.ForceSecureCookies)
}

func (handler *Handler) syncUserTeamsWithOAuthClaims(user *portainer.User, claims map[string]any, settings *portainer.OAuthSettings) error {
	if !settings.OAuthAutoMapTeamMemberships {
		return nil
	}

	claimName := settings.TeamMemberships.OAuthClaimName
	if claimName == "" {
		return nil
	}

	claimValue, ok := claims[claimName]
	if !ok {
		return nil
	}

	var claimGroups []string
	switch v := claimValue.(type) {
	case string:
		claimGroups = append(claimGroups, v)
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok {
				claimGroups = append(claimGroups, str)
			}
		}
	case []string:
		claimGroups = v
	}

	if len(claimGroups) == 0 {
		return nil
	}

	userMemberships, err := handler.DataStore.TeamMembership().TeamMembershipsByUserID(user.ID)
	if err != nil {
		return err
	}

	assignedTeams := make(map[portainer.TeamID]portainer.TeamMembershipID)
	for _, m := range userMemberships {
		assignedTeams[m.TeamID] = m.ID
	}

	shouldBeInTeams := make(map[portainer.TeamID]bool)
	matchedAnyRule := false

	for _, mapping := range settings.TeamMemberships.OAuthClaimMappings {
		if mapping.ClaimValRegex == "" || mapping.Team == 0 {
			continue
		}

		re, err := regexp.Compile(mapping.ClaimValRegex)
		if err != nil {
			log.Warn().Err(err).Str("regex", mapping.ClaimValRegex).Msg("failed to compile oauth claim value regex")
			continue
		}

		for _, group := range claimGroups {
			if re.MatchString(group) {
				shouldBeInTeams[mapping.Team] = true
				matchedAnyRule = true
			}
		}
	}

	for _, mapping := range settings.TeamMemberships.OAuthClaimMappings {
		teamID := mapping.Team
		if _, shouldBeIn := shouldBeInTeams[teamID]; !shouldBeIn {
			if membershipID, exists := assignedTeams[teamID]; exists {
				err := handler.DataStore.TeamMembership().Delete(membershipID)
				if err != nil {
					log.Warn().Err(err).Msg("unable to remove user team membership")
				}
			}
		}
	}

	for teamID := range shouldBeInTeams {
		if _, exists := assignedTeams[teamID]; !exists {
			membership := &portainer.TeamMembership{
				UserID: user.ID,
				TeamID: teamID,
				Role:   portainer.TeamMember,
			}
			err := handler.DataStore.TeamMembership().Create(membership)
			if err != nil {
				log.Warn().Err(err).Msg("unable to automatically sync user team with oauth")
			}
		}
	}

	if !matchedAnyRule && settings.DefaultTeamID != 0 {
		if _, exists := assignedTeams[settings.DefaultTeamID]; !exists {
			membership := &portainer.TeamMembership{
				UserID: user.ID,
				TeamID: settings.DefaultTeamID,
				Role:   portainer.TeamMember,
			}
			err := handler.DataStore.TeamMembership().Create(membership)
			if err != nil {
				log.Warn().Err(err).Msg("unable to add user to default team")
			}
		}
	}

	return nil
}
