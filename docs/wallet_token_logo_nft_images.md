# Wallet Token Logos And NFT Images (DTL Native)

This guide explains how the web wallet (`ui/index.html`) loads:
- token logos from `metadata_uri`
- NFT images from `token_uri` / `base_uri`

Contract runtime is not used. This is metadata-URI driven for native DTL assets.

## URI Policy
Allowed URI schemes:
- `https://`
- `ipfs://` (resolved to `https://ipfs.io/ipfs/<cid/path>`)
- `data:image/*`
- `http://` only for `localhost` / `127.0.0.1` (dev only)

Blocked:
- all other schemes
- non-localhost `http://`

## Token Logo Metadata (`metadata_uri`)
Wallet reads token metadata JSON and checks these keys in order:
1. `logo`
2. `logo_uri`
3. `image`
4. `image_url`

Example:

```json
{
  "name": "My Token",
  "symbol": "MYTK",
  "image": "ipfs://bafy.../token-logo.png"
}
```

## NFT Metadata

### NFT721
Image metadata source:
- `token_uri` if present
- otherwise `base_uri + token_id`

Metadata keys used:
1. `image`
2. `image_url`

Example:

```json
{
  "name": "Sword #12",
  "description": "Game item",
  "image": "https://cdn.example.com/nft/sword-12.png"
}
```

### NFT1155
Image metadata source:
- if `base_uri` contains `{id}`, wallet replaces it with 64-char lowercase hex token id
- else wallet uses `base_uri + token_id`

Example `base_uri`:

```text
ipfs://bafy.../metadata/{id}.json
```

## New Read APIs Used By Wallet

JSON-RPC:
- `dtl_listNFT721ByOwner(account, offset?, limit?, stateTag?)`
- `dtl_listNFT1155ByOwner(account, offset?, limit?, stateTag?)`

REST mirrors:
- `/dtl/nft721/owner`
- `/dtl/nft1155/owner`
- `/v1/dtl/nft721/owner`
- `/v1/dtl/nft1155/owner`

## Notes
- Wallet caches metadata for 10 minutes.
- Metadata fetch timeout is 5 seconds.
- On bad metadata/CORS/timeout, wallet shows a safe placeholder instead of breaking UI.
