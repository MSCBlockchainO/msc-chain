(function (global) {
  const wordlist = global.BIP39_WORDLIST || [];
  const wordMap = new Map();
  for (let i = 0; i < wordlist.length; i++) {
    wordMap.set(wordlist[i], i);
  }

  const encoder = new TextEncoder();

  const normalize = (str) => (str || "").normalize("NFKD");

  const bytesToBinary = (bytes) =>
    Array.from(bytes)
      .map((x) => x.toString(2).padStart(8, "0"))
      .join("");

  const binaryToByteArray = (binary) => {
    const bytes = [];
    for (let i = 0; i < binary.length / 8; i++) {
      bytes.push(parseInt(binary.slice(i * 8, i * 8 + 8), 2));
    }
    return new Uint8Array(bytes);
  };

  const sha256 = async (bytes) => {
    const hash = await crypto.subtle.digest("SHA-256", bytes);
    return new Uint8Array(hash);
  };

  const deriveChecksumBits = async (entropy) => {
    const hash = await sha256(entropy);
    const ent = entropy.length * 8;
    const cs = ent / 32;
    return bytesToBinary(hash).slice(0, cs);
  };

  const entropyToMnemonic = async (entropy) => {
    if (!(entropy instanceof Uint8Array)) {
      throw new Error("entropy must be Uint8Array");
    }
    if (entropy.length < 16 || entropy.length > 32 || entropy.length % 4 !== 0) {
      throw new Error("invalid entropy size");
    }

    const entropyBits = bytesToBinary(entropy);
    const checksumBits = await deriveChecksumBits(entropy);
    const bits = entropyBits + checksumBits;

    const chunks = bits.match(/.{1,11}/g) || [];
    const words = chunks.map((binary) => {
      const index = parseInt(binary, 2);
      return wordlist[index];
    });

    return words.join(" ");
  };

  const generateMnemonic = async (strength = 256) => {
    if (strength % 32 !== 0) {
      throw new Error("strength must be multiple of 32");
    }
    const entropy = new Uint8Array(strength / 8);
    crypto.getRandomValues(entropy);
    return entropyToMnemonic(entropy);
  };

  const mnemonicToEntropy = async (mnemonic) => {
    const words = normalize(mnemonic)
      .split(" ")
      .filter(Boolean);

    if (words.length % 3 !== 0) {
      throw new Error("invalid mnemonic length");
    }

    const bits = words
      .map((word) => {
        const index = wordMap.get(word);
        if (index === undefined) {
          throw new Error("word not in list");
        }
        return index.toString(2).padStart(11, "0");
      })
      .join("");

    const divider = Math.floor(bits.length / 33) * 32;
    const entropyBits = bits.slice(0, divider);
    const checksumBits = bits.slice(divider);

    const entropy = binaryToByteArray(entropyBits);
    const newChecksum = await deriveChecksumBits(entropy);
    if (newChecksum !== checksumBits) {
      throw new Error("checksum mismatch");
    }
    return entropy;
  };

  const validateMnemonic = async (mnemonic) => {
    try {
      await mnemonicToEntropy(mnemonic);
      return true;
    } catch (_err) {
      return false;
    }
  };

  const mnemonicToSeed = async (mnemonic, passphrase = "") => {
    const phrase = normalize(mnemonic);
    const salt = normalize("mnemonic" + passphrase);
    const keyMaterial = await crypto.subtle.importKey(
      "raw",
      encoder.encode(phrase),
      "PBKDF2",
      false,
      ["deriveBits"],
    );
    const bits = await crypto.subtle.deriveBits(
      {
        name: "PBKDF2",
        hash: "SHA-512",
        salt: encoder.encode(salt),
        iterations: 2048,
      },
      keyMaterial,
      512,
    );
    return new Uint8Array(bits);
  };

  global.bip39 = {
    generateMnemonic,
    mnemonicToSeed,
    validateMnemonic,
  };
})(window);
