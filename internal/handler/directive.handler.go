package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type directiveHandler struct{}

func RegisterDirective(api huma.API) {
	handler := &directiveHandler{}
	huma.Register(api, huma.Operation{
		OperationID: "encode-directive",
		Method:      http.MethodPost,
		Path:        "/directives/encode",
		Summary:     "Validate and encode a directive token",
	}, handler.encode)
	huma.Register(api, huma.Operation{
		OperationID: "decode-directive",
		Method:      http.MethodPost,
		Path:        "/directives/decode",
		Summary:     "Decode and validate a directive token",
	}, handler.decode)
	huma.Register(api, huma.Operation{
		OperationID: "validate-directive",
		Method:      http.MethodPost,
		Path:        "/directives/validate",
		Summary:     "Validate and normalize a directive document",
	}, handler.validate)
}

func (*directiveHandler) encode(_ context.Context, input *DirectiveEncodeInputDTO) (*DirectiveEncodeOutputDTO, error) {
	token, document, err := ToEncodedDirectiveDocument(input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid directive document")
	}
	return &DirectiveEncodeOutputDTO{Body: DirectiveCodecResponseDTO{Token: token, Document: document}}, nil
}

func (*directiveHandler) decode(_ context.Context, input *DirectiveDecodeInputDTO) (*DirectiveDecodeOutputDTO, error) {
	document, err := ToDecodedDirectiveDocument(input.Body.Token)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid directive token")
	}
	token, document, err := ToEncodedDirectiveDocument(document)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid directive token")
	}
	return &DirectiveDecodeOutputDTO{Body: DirectiveCodecResponseDTO{Token: token, Document: document}}, nil
}

func (*directiveHandler) validate(_ context.Context, input *DirectiveValidateInputDTO) (*DirectiveValidateOutputDTO, error) {
	_, document, err := ToEncodedDirectiveDocument(input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid directive document")
	}
	return &DirectiveValidateOutputDTO{Body: DirectiveValidationResponseDTO{Valid: true, Document: document}}, nil
}
