import unittest
import os
from security_crypto import encrypt, decrypt, get_master_key

class TestSecurityCrypto(unittest.TestCase):
    def setUp(self):
        # 32 bytes key
        self.master_key = b"12345678901234567890123456789012"
        os.environ["VAULT_MASTER_KEY"] = self.master_key.decode("utf-8")

    def test_encrypt_decrypt_roundtrip(self):
        original = b"sensitive-credentials-ssh"
        ciphertext, nonce = encrypt(original, self.master_key)
        
        # Verify encryption worked and produced ciphertext and nonce of expected size
        self.assertNotEqual(original, ciphertext)
        self.assertEqual(len(nonce), 12)
        
        # Decrypt
        decrypted = decrypt(ciphertext, nonce, self.master_key)
        self.assertEqual(original, decrypted)

    def test_get_master_key(self):
        key = get_master_key()
        self.assertEqual(key, self.master_key)

if __name__ == "__main__":
    unittest.main()
