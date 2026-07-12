package handler

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"

type DirectiveDocumentDTO struct {
	Kind    string                `json:"kind" enum:"inline,remote"`
	Payload *directive.Payload    `json:"payload,omitempty"`
	Remote  *directive.RemoteSpec `json:"remote,omitempty"`
}

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
