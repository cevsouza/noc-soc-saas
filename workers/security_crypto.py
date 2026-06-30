import os
from typing import Tuple
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

# AES-256-GCM requires a 32-byte key (256 bits)
MASTER_KEY_ENV = "VAULT_MASTER_KEY"

def get_master_key() -> bytes:
    key_str = os.environ.get(MASTER_KEY_ENV)
    if not key_str:
        raise ValueError(f"Environment variable {MASTER_KEY_ENV} is not set")
    key = key_str.encode("utf-8")
    if len(key) != 32:
        raise ValueError(f"{MASTER_KEY_ENV} must be exactly 32 bytes, current length: {len(key)}")
    return key

def encrypt(plain_text: bytes, key: bytes) -> Tuple[bytes, bytes]:
    """
    Encrypts plaintext bytes using AES-256-GCM.
    Returns a tuple of (ciphertext, nonce).
    This output is fully compatible with Go's cipher.NewGCM implementation.
    """
    if len(key) != 32:
        raise ValueError("Key must be exactly 32 bytes")
    aesgcm = AESGCM(key)
    nonce = os.urandom(12) # 12-byte standard nonce
    cipher_text = aesgcm.encrypt(nonce, plain_text, None)
    return cipher_text, nonce

def decrypt(cipher_text: bytes, nonce: bytes, key: bytes) -> bytes:
    """
    Decrypts AES-256-GCM encrypted ciphertext using the provided key and nonce.
    Expected ciphertext format matches Go's default GCM seal (ciphertext + 16-byte tag).
    """
    if len(key) != 32:
        raise ValueError("Key must be exactly 32 bytes")
    aesgcm = AESGCM(key)
    return aesgcm.decrypt(nonce, cipher_text, None)
