package handler

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"

func ToEncodedDirectiveDocument(document DirectiveDocumentDTO) (string, DirectiveDocumentDTO, error) {
	switch document.Kind {
	case "inline":
		if document.Payload == nil || document.Remote != nil {
			return "", DirectiveDocumentDTO{}, directive.ErrInvalidPayload
		}
		token, err := directive.Encode(*document.Payload)
		if err != nil {
			return "", DirectiveDocumentDTO{}, err
		}
		return token, DirectiveDocumentDTO{Kind: "inline", Payload: document.Payload}, nil
	case "remote":
		if document.Remote == nil || document.Payload != nil {
			return "", DirectiveDocumentDTO{}, directive.ErrInvalidPayload
		}
		token, err := directive.EncodeRemote(*document.Remote)
		if err != nil {
			return "", DirectiveDocumentDTO{}, err
		}
		decoded, err := directive.Decode(token)
		if err != nil {
			return "", DirectiveDocumentDTO{}, err
		}
		return token, DirectiveDocumentDTO{Kind: "remote", Remote: &decoded.Remote}, nil
	default:
		return "", DirectiveDocumentDTO{}, directive.ErrInvalidPayload
	}
}

func ToDecodedDirectiveDocument(token string) (DirectiveDocumentDTO, error) {
	decoded, err := directive.Decode(token)
	if err != nil {
		return DirectiveDocumentDTO{}, err
	}
	if decoded.Kind == directive.TokenRemote {
		return DirectiveDocumentDTO{Kind: "remote", Remote: &decoded.Remote}, nil
	}
	payload, err := directive.DecodePayload(decoded.Payload)
	if err != nil {
		return DirectiveDocumentDTO{}, err
	}
	if err := directive.Validate(payload); err != nil {
		return DirectiveDocumentDTO{}, err
	}
	return DirectiveDocumentDTO{Kind: "inline", Payload: &payload}, nil
}
