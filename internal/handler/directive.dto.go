package handler

import "github.com/lwmacct/260628-directive-proxy/internal/core/directive"

type DirectiveDocumentDTO = directive.Document

type DirectiveTokenRequestDTO struct {
	Token string `json:"token" minLength:"1"`
}

type DirectiveEncodeInputDTO struct {
	Body DirectiveDocumentDTO
}

type DirectiveDecodeInputDTO struct {
	Body DirectiveTokenRequestDTO
}

type DirectiveValidateInputDTO struct {
	Body DirectiveDocumentDTO
}

type DirectiveCodecResponseDTO struct {
	Token    string               `json:"token,omitempty"`
	Document DirectiveDocumentDTO `json:"document"`
}

type DirectiveValidationResponseDTO struct {
	Valid    bool                 `json:"valid"`
	Document DirectiveDocumentDTO `json:"document"`
}

type DirectiveEncodeOutputDTO struct {
	Body DirectiveCodecResponseDTO
}

type DirectiveDecodeOutputDTO struct {
	Body DirectiveCodecResponseDTO
}

type DirectiveValidateOutputDTO struct {
	Body DirectiveValidationResponseDTO
}
