package main

import (
	"errors"
	"log"

	"github.com/cacggghp/vk-turn-proxy/internal/providerstate"
	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
)

type ProviderCredentialError struct {
	Diagnosis providerstate.Diagnosis
	cause     error
}

func newProviderCredentialError(provider statusmodel.ProviderName, err error) error {
	if err == nil {
		return nil
	}
	return &ProviderCredentialError{
		Diagnosis: providerstate.ClassifyError(provider, err),
		cause:     err,
	}
}

func (e *ProviderCredentialError) Error() string {
	if e == nil || e.cause == nil {
		return "provider credential error"
	}
	return e.cause.Error()
}

func (e *ProviderCredentialError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func providerCredentialDiagnosis(err error) (providerstate.Diagnosis, bool) {
	var providerErr *ProviderCredentialError
	if !errors.As(err, &providerErr) || providerErr == nil {
		return providerstate.Diagnosis{}, false
	}
	return providerErr.Diagnosis, true
}

func logProviderCredentialDiagnosis(streamID int, err error) {
	diagnosis, ok := providerCredentialDiagnosis(err)
	if !ok {
		return
	}
	log.Printf(
		"[STREAM %d] [Provider %s] state=%s code=%s retryable=%t message=%s",
		streamID,
		diagnosis.Provider,
		diagnosis.State,
		diagnosis.Code,
		diagnosis.Retryable,
		diagnosis.Message,
	)
}
