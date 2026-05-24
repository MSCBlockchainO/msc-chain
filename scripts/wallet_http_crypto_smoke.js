#!/usr/bin/env node
"use strict";

const assert = require("assert");
const fs = require("fs");
const path = require("path");
const vm = require("vm");
const nodeCrypto = require("crypto");

const repoRoot = path.resolve(__dirname, "..");
const read = (relativePath) => fs.readFileSync(path.join(repoRoot, relativePath), "utf8");

const context = {
  ArrayBuffer,
  DataView,
  Float64Array,
  Map,
  Math,
  TextDecoder,
  TextEncoder,
  Uint8Array,
  Uint32Array,
  console,
  crypto: {
    getRandomValues(buf) {
      return nodeCrypto.webcrypto.getRandomValues(buf);
    },
  },
};
context.window = context;
context.self = context;
context.globalThis = context;
vm.createContext(context);

for (const file of [
  "ui/vendor/nacl.min.js",
  "ui/vendor/msc_crypto_fallback.js",
  "ui/vendor/bip39_wordlist.js",
  "ui/vendor/bip39-lite.js",
]) {
  vm.runInContext(read(file), context, { filename: file });
}

const hex = (bytes) =>
  Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");

(async () => {
  assert.strictEqual(context.crypto.subtle, undefined, "test must simulate insecure HTTP crypto");
  assert.ok(context.MSC_CRYPTO_FALLBACK, "fallback not loaded");
  assert.ok(context.bip39, "bip39 helper not loaded");
  assert.ok(context.nacl, "tweetnacl not loaded");

  const sha = context.MSC_CRYPTO_FALLBACK.sha256(new TextEncoder().encode("abc"));
  assert.strictEqual(
    hex(sha),
    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  );

  const vectorMnemonic =
    "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about";
  const vectorSeed = await context.bip39.mnemonicToSeed(vectorMnemonic, "TREZOR");
  assert.strictEqual(
    hex(vectorSeed),
    "c55257c360c07c72029aebc1b53c05ed0362ada38ead3e3e9efa3708e53495531f09a6987599d18264c1e1c92f2cf141630c7a3c4ab7c81b2f001698e7463b04",
  );

  const mnemonic = await context.bip39.generateMnemonic(256);
  assert.strictEqual(mnemonic.split(/\s+/).length, 24, "generated mnemonic must be 24 words");
  assert.strictEqual(await context.bip39.validateMnemonic(mnemonic), true);

  const key = context.MSC_CRYPTO_FALLBACK.pbkdf2HmacSha512(
    new TextEncoder().encode("wallet-password"),
    new TextEncoder().encode("wallet-salt"),
    128,
    context.nacl.secretbox.keyLength,
  );
  const nonce = context.crypto.getRandomValues(new Uint8Array(context.nacl.secretbox.nonceLength));
  const payload = new TextEncoder().encode("wallet-secret");
  const boxed = context.nacl.secretbox(payload, nonce, key);
  const opened = context.nacl.secretbox.open(boxed, nonce, key);
  assert.strictEqual(new TextDecoder().decode(opened), "wallet-secret");

  console.log("wallet-http-crypto-smoke ok");
})();
