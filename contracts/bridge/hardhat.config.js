import { defineConfig } from "hardhat/config";

export default defineConfig({
  networks: {
    simulated: {
      type: "edr-simulated",
      chainType: "l1",
      chainId: 56,
      hardfork: "cancun",
    },
    tronSimulated: {
      type: "edr-simulated",
      chainType: "l1",
      chainId: 728126428,
      hardfork: "cancun",
    },
  },
});
