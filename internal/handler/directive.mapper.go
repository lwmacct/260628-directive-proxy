package handler

import "github.com/lwmacct/260628-directive-proxy/internal/core/directive"

func ToEncodedDirectiveDocument(document DirectiveDocumentDTO) (string, DirectiveDocumentDTO, error) {
	normalized, err := directive.ValidateDocument(document)
	if err != nil {
		return "", DirectiveDocumentDTO{}, err
	}
	token, err := directive.EncodeDocument(normalized)
	return token, normalized, err
}

func ToDecodedDirectiveDocument(token string) (DirectiveDocumentDTO, error) {
	return directive.Decode(token)
}
