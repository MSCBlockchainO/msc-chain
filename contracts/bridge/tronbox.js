export const contracts_build_directory = "./artifacts-tron";
export const build_info_directory = "./artifacts-tron-build-info";

export const compilers = {
  solc: {
    version: "0.8.26",
    settings: {
      optimizer: { enabled: true, runs: 1_000 },
      evmVersion: "cancun",
      metadata: { bytecodeHash: "none" },
    },
  },
};
