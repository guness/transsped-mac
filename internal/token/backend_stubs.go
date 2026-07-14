package token

import "github.com/miekg/pkcs11"

// The methods below complete the pkcs11mod.Backend interface but have no
// use case for a cloud-signing-only token: PIN management, session-state
// save/restore, object mutation, encryption/decryption, digesting,
// signature verification, streaming sign/encrypt combos, key
// generation/wrapping/derivation, and randomness. Each returns
// CKR_FUNCTION_NOT_SUPPORTED (or the interface's zero value where no error
// is returned).

func (b *Backend) InitPIN(pkcs11.SessionHandle, string) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SetPIN(pkcs11.SessionHandle, string, string) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) CloseAllSessions(uint) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) GetOperationState(pkcs11.SessionHandle) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SetOperationState(pkcs11.SessionHandle, []byte, pkcs11.ObjectHandle, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) CreateObject(pkcs11.SessionHandle, []*pkcs11.Attribute) (pkcs11.ObjectHandle, error) {
	return 0, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) CopyObject(pkcs11.SessionHandle, pkcs11.ObjectHandle, []*pkcs11.Attribute) (pkcs11.ObjectHandle, error) {
	return 0, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DestroyObject(pkcs11.SessionHandle, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) GetObjectSize(pkcs11.SessionHandle, pkcs11.ObjectHandle) (uint, error) {
	return 0, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SetAttributeValue(pkcs11.SessionHandle, pkcs11.ObjectHandle, []*pkcs11.Attribute) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) EncryptInit(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) Encrypt(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) EncryptUpdate(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) EncryptFinal(pkcs11.SessionHandle) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DecryptInit(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) Decrypt(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DecryptUpdate(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DecryptFinal(pkcs11.SessionHandle) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DigestInit(pkcs11.SessionHandle, []*pkcs11.Mechanism) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) Digest(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DigestUpdate(pkcs11.SessionHandle, []byte) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DigestKey(pkcs11.SessionHandle, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DigestFinal(pkcs11.SessionHandle) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SignUpdate(pkcs11.SessionHandle, []byte) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SignFinal(pkcs11.SessionHandle) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SignRecoverInit(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SignRecover(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) VerifyInit(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) Verify(pkcs11.SessionHandle, []byte, []byte) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) VerifyUpdate(pkcs11.SessionHandle, []byte) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) VerifyFinal(pkcs11.SessionHandle, []byte) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) VerifyRecoverInit(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) VerifyRecover(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DigestEncryptUpdate(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DecryptDigestUpdate(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SignEncryptUpdate(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DecryptVerifyUpdate(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) GenerateKey(pkcs11.SessionHandle, []*pkcs11.Mechanism, []*pkcs11.Attribute) (pkcs11.ObjectHandle, error) {
	return 0, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) GenerateKeyPair(pkcs11.SessionHandle, []*pkcs11.Mechanism, []*pkcs11.Attribute, []*pkcs11.Attribute) (pkcs11.ObjectHandle, pkcs11.ObjectHandle, error) {
	return 0, 0, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) WrapKey(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle, pkcs11.ObjectHandle) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) UnwrapKey(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle, []byte, []*pkcs11.Attribute) (pkcs11.ObjectHandle, error) {
	return 0, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) DeriveKey(pkcs11.SessionHandle, []*pkcs11.Mechanism, pkcs11.ObjectHandle, []*pkcs11.Attribute) (pkcs11.ObjectHandle, error) {
	return 0, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) SeedRandom(pkcs11.SessionHandle, []byte) error {
	return pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) GenerateRandom(pkcs11.SessionHandle, int) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
func (b *Backend) WaitForSlotEvent(uint) chan pkcs11.SlotEvent {
	return nil
}
