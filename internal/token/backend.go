package token

import (
	"tscloud/internal/csc"

	"github.com/miekg/pkcs11"
)

const slotID = 0

// Backend implements pkcs11mod.Backend, exposing a single Trans Sped Cloud
// signing credential (leaf certificate + private key + intermediates) as an
// in-memory PKCS#11 token. Signing operations are forwarded to the CSC API
// via csc.Signer.
type Backend struct {
	objs    []*Object
	signer  *csc.Signer
	pin     string // cached at Login; also fed to signer via PIN()
	find    []pkcs11.ObjectHandle
	signKey pkcs11.ObjectHandle
}

// NewBackend constructs a Backend serving objs and signing via signer. It
// wires signer.PIN to read the PIN cached by the most recent Login call.
func NewBackend(objs []*Object, signer *csc.Signer) *Backend {
	bk := &Backend{objs: objs, signer: signer}
	signer.PIN = func() string { return bk.pin }
	return bk
}

func ckErr(code uint) error { return pkcs11.Error(code) }

func (b *Backend) Initialize() error { return nil }
func (b *Backend) Finalize() error   { return nil }

func (b *Backend) GetInfo() (pkcs11.Info, error) {
	return pkcs11.Info{ManufacturerID: "Trans Sped", LibraryDescription: "TS Cloud PKCS#11"}, nil
}
func (b *Backend) GetSlotList(bool) ([]uint, error) { return []uint{slotID}, nil }
func (b *Backend) GetSlotInfo(uint) (pkcs11.SlotInfo, error) {
	return pkcs11.SlotInfo{SlotDescription: "TS Cloud", Flags: pkcs11.CKF_TOKEN_PRESENT}, nil
}
func (b *Backend) GetTokenInfo(uint) (pkcs11.TokenInfo, error) {
	return pkcs11.TokenInfo{Label: "Trans Sped Cloud", ManufacturerID: "Trans Sped",
		Flags: pkcs11.CKF_TOKEN_INITIALIZED | pkcs11.CKF_LOGIN_REQUIRED | pkcs11.CKF_USER_PIN_INITIALIZED}, nil
}
func (b *Backend) GetMechanismList(uint) ([]*pkcs11.Mechanism, error) {
	return []*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_RSA_PKCS, nil)}, nil
}
func (b *Backend) GetMechanismInfo(uint, []*pkcs11.Mechanism) (pkcs11.MechanismInfo, error) {
	return pkcs11.MechanismInfo{MinKeySize: 2048, MaxKeySize: 4096, Flags: pkcs11.CKF_SIGN}, nil
}
func (b *Backend) OpenSession(uint, uint) (pkcs11.SessionHandle, error) { return 1, nil }
func (b *Backend) CloseSession(pkcs11.SessionHandle) error              { return nil }
func (b *Backend) GetSessionInfo(pkcs11.SessionHandle) (pkcs11.SessionInfo, error) {
	return pkcs11.SessionInfo{SlotID: slotID, State: pkcs11.CKS_RO_USER_FUNCTIONS}, nil
}
func (b *Backend) Login(_ pkcs11.SessionHandle, _ uint, pin string) error { b.pin = pin; return nil }
func (b *Backend) Logout(pkcs11.SessionHandle) error                      { b.pin = ""; return nil }

func (b *Backend) FindObjectsInit(_ pkcs11.SessionHandle, tmpl []*pkcs11.Attribute) error {
	b.find = nil
	for _, o := range b.objs {
		if o.Matches(tmpl) {
			b.find = append(b.find, o.Handle)
		}
	}
	return nil
}
func (b *Backend) FindObjects(_ pkcs11.SessionHandle, max int) ([]pkcs11.ObjectHandle, bool, error) {
	n := len(b.find)
	if max >= 0 && n > max {
		n = max
	}
	out := b.find[:n]
	b.find = b.find[n:]
	return out, false, nil
}
func (b *Backend) FindObjectsFinal(pkcs11.SessionHandle) error { b.find = nil; return nil }

func (b *Backend) GetAttributeValue(_ pkcs11.SessionHandle, h pkcs11.ObjectHandle, tmpl []*pkcs11.Attribute) ([]*pkcs11.Attribute, error) {
	var obj *Object
	for _, o := range b.objs {
		if o.Handle == h {
			obj = o
			break
		}
	}
	if obj == nil {
		return nil, ckErr(pkcs11.CKR_OBJECT_HANDLE_INVALID)
	}
	out := make([]*pkcs11.Attribute, 0, len(tmpl))
	for _, a := range tmpl {
		if v, ok := obj.Attrs[a.Type]; ok {
			out = append(out, pkcs11.NewAttribute(a.Type, v))
		} else {
			out = append(out, pkcs11.NewAttribute(a.Type, nil)) // absent
		}
	}
	return out, nil
}

func (b *Backend) SignInit(_ pkcs11.SessionHandle, _ []*pkcs11.Mechanism, key pkcs11.ObjectHandle) error {
	b.signKey = key
	return nil
}
func (b *Backend) Sign(_ pkcs11.SessionHandle, data []byte) ([]byte, error) {
	sig, err := b.signer.SignDigestInfo(data)
	if err != nil {
		return nil, ckErr(pkcs11.CKR_FUNCTION_FAILED)
	}
	return sig, nil
}
