package directive

import directivecontract "github.com/lwmacct/260628-directive-proxy/pkg/directive"

const (
	TokenFamily  = directivecontract.TokenFamily
	TokenVersion = directivecontract.TokenVersion
	TokenInline  = directivecontract.TokenInline
	TokenRemote  = directivecontract.TokenRemote
)

const (
	KindInline = "inline"
	KindRemote = "remote"
)

type Document struct {
	Kind    string      `json:"kind" enum:"inline,remote"`
	Payload *Payload    `json:"payload,omitempty"`
	Remote  *RemoteSpec `json:"remote,omitempty"`
}

type RecoverySpec = directivecontract.RecoverySpec
type RecoveryTriggerSpec = directivecontract.RecoveryTriggerSpec
type RecoveryUnexpectedStatusSpec = directivecontract.RecoveryUnexpectedStatusSpec
type RecoveryStatusRangeSpec = directivecontract.RecoveryStatusRangeSpec
type RecoveryBudgetSpec = directivecontract.RecoveryBudgetSpec

const (
	RemoteTypeHTTP  = directivecontract.RemoteTypeHTTP
	RemoteTypeRedis = directivecontract.RemoteTypeRedis
	RemoteTypeFile  = directivecontract.RemoteTypeFile

	maxRemoteKeyBytes = directivecontract.MaxRemoteKeyBytes
)

type HeaderSide = directivecontract.HeaderSide
type HeaderAction = directivecontract.HeaderAction
type RemoteSpec = directivecontract.RemoteSpec
type HTTPRemoteSpec = directivecontract.HTTPRemoteSpec
type RedisRemoteSpec = directivecontract.RedisRemoteSpec
type FileRemoteSpec = directivecontract.FileRemoteSpec
type Payload = directivecontract.Payload
type BodyStoreSpec = directivecontract.BodyStoreSpec
type TargetSection = directivecontract.TargetSection
type HeaderPolicy = directivecontract.HeaderPolicy
type HeaderMutation = directivecontract.HeaderMutation

const (
	HeaderSideRequest  = directivecontract.HeaderSideRequest
	HeaderSideResponse = directivecontract.HeaderSideResponse
	HeaderActionAdd    = directivecontract.HeaderActionAdd
	HeaderActionSet    = directivecontract.HeaderActionSet
	HeaderActionDel    = directivecontract.HeaderActionDel
)
