import requests
import json
import hashlib
import time
from nacl.signing import SigningKey
from nacl.exceptions import BadSignatureError

def sign_transaction_correctly():
    """Sign transaction exactly as Go code expects"""
    
    print("🔐 SIGNING TRANSACTION (GO-COMPATIBLE)")
    print("="*60)
    
    # Step 1: Generate keypair
    print("\n1️⃣ Generating keypair...")
    import secrets
    seed = secrets.token_bytes(32)
    signing_key = SigningKey(seed)
    verify_key = signing_key.verify_key
    
    public_key = verify_key.encode().hex()
    private_key = signing_key.encode().hex()
    
    # Derive address (matching Go's AddressFromPublicKey)
    address = hashlib.sha256(bytes.fromhex(public_key)).hexdigest()[:40]
    
    print(f"✅ Keypair generated!")
    print(f"   Public Key: {public_key}")
    print(f"   Private Key: {private_key[:32]}...")
    print(f"   Address: {address}")
    
    # Step 2: Create receiver
    print("\n2️⃣ Creating receiver...")
    receiver_resp = requests.get("http://localhost:8080/createWallet")
    receiver = receiver_resp.json()['address']
    print(f"✅ Receiver: {receiver}")
    
    # Step 3: Get parameters
    amount = 100
    nonce = 1
    fee = 1
    chain_id = "msc-testnet-1"
    
    # Step 4: Create payload EXACTLY as Go does
    payload = f"{address}|{receiver}|{amount}|{nonce}|{chain_id}"
    print(f"\n📝 Payload (raw): {payload}")
    
    # Step 5: Hash the payload (SHA256) - Go does this in Verify function
    payload_bytes = payload.encode('utf-8')
    payload_hash = hashlib.sha256(payload_bytes).digest()
    print(f"📝 Payload hash (SHA256): {payload_hash.hex()}")
    
    # Step 6: Sign the HASH (not raw payload)
    signature = signing_key.sign(payload_hash).signature
    print(f"✅ Signature (of hash): {signature.hex()}")
    
    # Step 7: Create transaction ID (hash of payload)
    tx_id = hashlib.sha256(payload_bytes).hexdigest()
    
    # Step 8: Create full transaction
    transaction = {
        "id": tx_id,
        "from": address,
        "to": receiver,
        "amount": amount,
        "nonce": nonce,
        "publicKey": public_key,
        "signature": signature.hex(),
        "fee": fee,
        "type": "transfer"
    }
    
    print(f"\n📄 Final Transaction:")
    print(json.dumps(transaction, indent=2))
    
    # Step 9: Verify locally (matching Go's logic)
    print(f"\n🔍 Local verification...")
    try:
        # Go does: Verify(publicKey, payload, signature)
        # Where it hashes payload internally
        verify_key.verify(payload_hash, signature)
        print(f"✅ Local verification PASSED!")
    except BadSignatureError:
        print(f"❌ Local verification FAILED!")
        return None
    
    return transaction

def test_with_existing_wallet():
    """Test with the wallet that already has balance"""
    
    print("\n" + "="*60)
    print("💰 TESTING WITH EXISTING WALLET (46e8cf9565585c67a09eb0769e5e0f04fe61425f)")
    print("="*60)
    
    # Problem: We don't have private key for existing wallet
    # Solution: Need to get it from secure_wallet.json or CLI
    
    print("\n🔍 Checking if we can extract keys from CLI wallet...")
    
    # Try to read secure wallet
    import os
    wallet_path = os.path.expanduser("~/.msc/secure_wallet.json")
    
    if os.path.exists(wallet_path):
        print(f"✅ Found secure wallet at: {wallet_path}")
        with open(wallet_path, 'r') as f:
            wallet_data = json.load(f)
        
        print(f"   Address: {wallet_data.get('address')}")
        print(f"   Public Key: {wallet_data.get('publicKey')}")
        print(f"   Crypto data present: {'Ciphertext' in wallet_data.get('crypto', {})}")
        
        # The private key is encrypted - need password to decrypt
        print(f"\n🔐 Private key is encrypted with password")
        print(f"   Need to decrypt using password 'mubasera'")
        
    else:
        print(f"❌ Secure wallet not found at: {wallet_path}")
        print(f"   Looking for: {wallet_path}")
    
    return None

def send_test_transaction_with_bypass():
    """Send transaction with validation bypass"""
    
    print("\n" + "="*60)
    print("🚀 SENDING TEST TRANSACTION (WITH BYPASS)")
    print("="*60)
    
    # Create a properly signed transaction
    transaction = sign_transaction_correctly()
    if not transaction:
        print("❌ Failed to create transaction")
        return
    
    # Try different endpoints
    endpoints = ["/sendTx", "/submitTx", "/testTx"]
    
    for endpoint in endpoints:
        print(f"\n📤 Trying {endpoint}...")
        try:
            response = requests.post(
                f"http://localhost:8080{endpoint}",
                json=transaction,
                timeout=10
            )
            
            print(f"   Status: {response.status_code}")
            print(f"   Response: {response.text[:100]}")
            
            if response.status_code == 200:
                print(f"   🎉 SUCCESS with {endpoint}!")
                break
                
        except Exception as e:
            print(f"   ❌ Error: {type(e).__name__}")

if __name__ == "__main__":
    # First, test signing correctness
    sign_transaction_correctly()
    
    # Then check existing wallet
    test_with_existing_wallet()
    
    # Finally try to send
    send_test_transaction_with_bypass()





























