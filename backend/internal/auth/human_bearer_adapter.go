package auth

import (
	"context"
	"errors"
)

func AuthenticateHumanBearer(
	ctx context.Context,
	verifier *HumanVerifier,
	rawToken string,
) (HumanPrincipal, error) {
	if verifier == nil {
		return HumanPrincipal{}, errors.New(
			"human authentication is not configured",
		)
	}

	return verifier.Verify(ctx, rawToken)
}
