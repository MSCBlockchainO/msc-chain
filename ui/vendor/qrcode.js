// Minimal QR code generator (based on qrcode-generator by Kazuhiko Arase, MIT License).
// Exposes window.MSCQRCode.toDataURL(text, size)
(function () {
  "use strict";

  function QR8bitByte(data) {
    this.mode = 1;
    this.data = data;
    this.parsedData = [];
    for (var i = 0, l = data.length; i < l; i++) {
      var byteArray = [];
      var code = data.charCodeAt(i);
      if (code > 0x10000) {
        byteArray[0] = 0xf0 | ((code & 0x1c0000) >>> 18);
        byteArray[1] = 0x80 | ((code & 0x3f000) >>> 12);
        byteArray[2] = 0x80 | ((code & 0xfc0) >>> 6);
        byteArray[3] = 0x80 | (code & 0x3f);
      } else if (code > 0x800) {
        byteArray[0] = 0xe0 | ((code & 0xf000) >>> 12);
        byteArray[1] = 0x80 | ((code & 0xfc0) >>> 6);
        byteArray[2] = 0x80 | (code & 0x3f);
      } else if (code > 0x80) {
        byteArray[0] = 0xc0 | ((code & 0x7c0) >>> 6);
        byteArray[1] = 0x80 | (code & 0x3f);
      } else {
        byteArray[0] = code;
      }
      this.parsedData.push(byteArray);
    }
    this.parsedData = Array.prototype.concat.apply([], this.parsedData);
    this.getLength = function () {
      return this.parsedData.length;
    };
    this.write = function (buffer) {
      for (var i = 0, l = this.parsedData.length; i < l; i++) {
        buffer.put(this.parsedData[i], 8);
      }
    };
  }

  var QRMode = {
    MODE_8BIT_BYTE: 1,
  };

  var QRErrorCorrectionLevel = {
    L: 1,
    M: 0,
    Q: 3,
    H: 2,
  };

  function QRRSBlock(totalCount, dataCount) {
    this.totalCount = totalCount;
    this.dataCount = dataCount;
  }

  var QRRSBlockHelper = {
    getRSBlocks: function (typeNumber, errorCorrectionLevel) {
      var rsBlock = QRRSBlockHelper.RS_BLOCK_TABLE[(typeNumber - 1) * 4 + errorCorrectionLevel];
      if (typeof rsBlock === "undefined") {
        throw new Error("bad rs block @ typeNumber:" + typeNumber + "/errorCorrectionLevel:" + errorCorrectionLevel);
      }
      var length = rsBlock.length / 3;
      var list = [];
      for (var i = 0; i < length; i++) {
        var count = rsBlock[i * 3 + 0];
        var totalCount = rsBlock[i * 3 + 1];
        var dataCount = rsBlock[i * 3 + 2];
        for (var j = 0; j < count; j++) {
          list.push(new QRRSBlock(totalCount, dataCount));
        }
      }
      return list;
    },
  };

  QRRSBlockHelper.RS_BLOCK_TABLE = [
    [1, 26, 19], [1, 26, 16], [1, 26, 13], [1, 26, 9],
    [1, 44, 34], [1, 44, 28], [1, 44, 22], [1, 44, 16],
    [1, 70, 55], [1, 70, 44], [2, 35, 17], [2, 35, 13],
    [1, 100, 80], [2, 50, 32], [2, 50, 24], [4, 25, 9],
    [1, 134, 108], [2, 67, 43], [2, 33, 15, 2, 34, 16], [2, 33, 11, 2, 34, 12],
    [2, 86, 68], [4, 43, 27], [4, 43, 19], [4, 43, 15],
    [2, 98, 78], [4, 49, 31], [2, 32, 14, 4, 33, 15], [4, 39, 13, 1, 40, 14],
    [2, 121, 97], [2, 60, 38, 2, 61, 39], [4, 40, 18, 2, 41, 19], [4, 40, 14, 2, 41, 15],
    [2, 146, 116], [3, 58, 36, 2, 59, 37], [4, 36, 16, 4, 37, 17], [4, 36, 12, 4, 37, 13],
    [2, 86, 68, 2, 87, 69], [4, 69, 43, 1, 70, 44], [6, 43, 19, 2, 44, 20], [6, 43, 15, 2, 44, 16],
  ];

  function QRBitBuffer() {
    this.buffer = [];
    this.length = 0;
  }
  QRBitBuffer.prototype = {
    get: function (index) {
      var bufIndex = Math.floor(index / 8);
      return ((this.buffer[bufIndex] >>> (7 - (index % 8))) & 1) === 1;
    },
    put: function (num, length) {
      for (var i = 0; i < length; i++) {
        this.putBit(((num >>> (length - i - 1)) & 1) === 1);
      }
    },
    putBit: function (bit) {
      var bufIndex = Math.floor(this.length / 8);
      if (this.buffer.length <= bufIndex) {
        this.buffer.push(0);
      }
      if (bit) {
        this.buffer[bufIndex] |= 0x80 >>> (this.length % 8);
      }
      this.length++;
    },
  };

  var QRUtil = {
    PATTERN_POSITION_TABLE: [[], [6, 18], [6, 22], [6, 26], [6, 30], [6, 34]],
    G15: 1 << 10 | 1 << 8 | 1 << 5 | 1 << 4 | 1 << 2 | 1 << 1 | 1,
    G15_MASK: 1 << 14 | 1 << 12 | 1 << 10 | 1 << 4 | 1 << 1,
    getBCHTypeInfo: function (data) {
      var d = data << 10;
      while (QRUtil.getBCHDigit(d) - QRUtil.getBCHDigit(QRUtil.G15) >= 0) {
        d ^= QRUtil.G15 << (QRUtil.getBCHDigit(d) - QRUtil.getBCHDigit(QRUtil.G15));
      }
      return ((data << 10) | d) ^ QRUtil.G15_MASK;
    },
    getBCHDigit: function (data) {
      var digit = 0;
      while (data !== 0) {
        digit++;
        data >>>= 1;
      }
      return digit;
    },
    getMask: function (maskPattern, i, j) {
      switch (maskPattern) {
        case 0: return (i + j) % 2 === 0;
        case 1: return i % 2 === 0;
        case 2: return j % 3 === 0;
        case 3: return (i + j) % 3 === 0;
        case 4: return (Math.floor(i / 2) + Math.floor(j / 3)) % 2 === 0;
        case 5: return (i * j) % 2 + (i * j) % 3 === 0;
        case 6: return ((i * j) % 2 + (i * j) % 3) % 2 === 0;
        case 7: return ((i * j) % 3 + (i + j) % 2) % 2 === 0;
        default: throw new Error("bad maskPattern:" + maskPattern);
      }
    },
  };

  function QRCode(typeNumber, errorCorrectionLevel) {
    this.typeNumber = typeNumber;
    this.errorCorrectionLevel = errorCorrectionLevel;
    this.modules = null;
    this.moduleCount = 0;
    this.dataCache = null;
    this.dataList = [];
  }

  QRCode.prototype = {
    addData: function (data) {
      var newData = new QR8bitByte(data);
      this.dataList.push(newData);
      this.dataCache = null;
    },
    isDark: function (row, col) {
      if (row < 0 || this.moduleCount <= row || col < 0 || this.moduleCount <= col) {
        throw new Error(row + "," + col);
      }
      return this.modules[row][col];
    },
    getModuleCount: function () {
      return this.moduleCount;
    },
    make: function () {
      if (this.typeNumber < 1) {
        this.typeNumber = 1;
      }
      this.makeImpl(false, 0);
    },
    makeImpl: function (test, maskPattern) {
      this.moduleCount = this.typeNumber * 4 + 17;
      this.modules = new Array(this.moduleCount);
      for (var row = 0; row < this.moduleCount; row++) {
        this.modules[row] = new Array(this.moduleCount);
        for (var col = 0; col < this.moduleCount; col++) {
          this.modules[row][col] = null;
        }
      }
      this.setupPositionProbePattern(0, 0);
      this.setupPositionProbePattern(this.moduleCount - 7, 0);
      this.setupPositionProbePattern(0, this.moduleCount - 7);
      this.setupTimingPattern();
      this.setupTypeInfo(test, maskPattern);
      if (this.dataCache === null) {
        this.dataCache = this.createData(this.typeNumber, this.errorCorrectionLevel, this.dataList);
      }
      this.mapData(this.dataCache, maskPattern);
    },
    setupPositionProbePattern: function (row, col) {
      for (var r = -1; r <= 7; r++) {
        if (row + r <= -1 || this.moduleCount <= row + r) continue;
        for (var c = -1; c <= 7; c++) {
          if (col + c <= -1 || this.moduleCount <= col + c) continue;
          if ((0 <= r && r <= 6 && (c === 0 || c === 6)) ||
              (0 <= c && c <= 6 && (r === 0 || r === 6)) ||
              (2 <= r && r <= 4 && 2 <= c && c <= 4)) {
            this.modules[row + r][col + c] = true;
          } else {
            this.modules[row + r][col + c] = false;
          }
        }
      }
    },
    setupTimingPattern: function () {
      for (var i = 8; i < this.moduleCount - 8; i++) {
        if (this.modules[i][6] === null) {
          this.modules[i][6] = i % 2 === 0;
        }
        if (this.modules[6][i] === null) {
          this.modules[6][i] = i % 2 === 0;
        }
      }
    },
    setupTypeInfo: function (test, maskPattern) {
      var data = (QRErrorCorrectionLevel.M << 3) | maskPattern;
      var bits = QRUtil.getBCHTypeInfo(data);
      for (var i = 0; i < 15; i++) {
        var mod = !test && ((bits >> i) & 1) === 1;
        if (i < 6) {
          this.modules[i][8] = mod;
        } else if (i < 8) {
          this.modules[i + 1][8] = mod;
        } else {
          this.modules[this.moduleCount - 15 + i][8] = mod;
        }
        if (i < 8) {
          this.modules[8][this.moduleCount - i - 1] = mod;
        } else if (i < 9) {
          this.modules[8][15 - i - 1 + 1] = mod;
        } else {
          this.modules[8][15 - i - 1] = mod;
        }
      }
      this.modules[this.moduleCount - 8][8] = !test;
    },
    mapData: function (data, maskPattern) {
      var inc = -1;
      var row = this.moduleCount - 1;
      var bitIndex = 7;
      var byteIndex = 0;
      for (var col = this.moduleCount - 1; col > 0; col -= 2) {
        if (col === 6) col--;
        while (true) {
          for (var c = 0; c < 2; c++) {
            if (this.modules[row][col - c] === null) {
              var dark = false;
              if (byteIndex < data.length) {
                dark = ((data[byteIndex] >>> bitIndex) & 1) === 1;
              }
              var mask = QRUtil.getMask(maskPattern, row, col - c);
              if (mask) dark = !dark;
              this.modules[row][col - c] = dark;
              bitIndex--;
              if (bitIndex === -1) {
                byteIndex++;
                bitIndex = 7;
              }
            }
          }
          row += inc;
          if (row < 0 || this.moduleCount <= row) {
            row -= inc;
            inc = -inc;
            break;
          }
        }
      }
    },
    createData: function (typeNumber, errorCorrectionLevel, dataList) {
      var rsBlocks = QRRSBlockHelper.getRSBlocks(typeNumber, errorCorrectionLevel);
      var buffer = new QRBitBuffer();
      for (var i = 0; i < dataList.length; i++) {
        var data = dataList[i];
        buffer.put(QRMode.MODE_8BIT_BYTE, 4);
        buffer.put(data.getLength(), 8);
        data.write(buffer);
      }
      var totalDataCount = 0;
      for (var r = 0; r < rsBlocks.length; r++) {
        totalDataCount += rsBlocks[r].dataCount;
      }
      if (buffer.length + 4 <= totalDataCount * 8) {
        buffer.put(0, 4);
      }
      while (buffer.length % 8 !== 0) {
        buffer.putBit(false);
      }
      var paddingBytes = [0xec, 0x11];
      var paddingIndex = 0;
      while (buffer.length < totalDataCount * 8) {
        buffer.put(paddingBytes[paddingIndex % 2], 8);
        paddingIndex++;
      }
      return buffer.buffer;
    },
    createDataURL: function (cellSize, margin) {
      cellSize = cellSize || 4;
      margin = margin || 2;
      var size = this.getModuleCount() * cellSize + margin * 2;
      var canvas = document.createElement("canvas");
      canvas.width = size;
      canvas.height = size;
      var ctx = canvas.getContext("2d");
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(0, 0, size, size);
      ctx.fillStyle = "#0a0a0a";
      for (var row = 0; row < this.getModuleCount(); row++) {
        for (var col = 0; col < this.getModuleCount(); col++) {
          if (this.isDark(row, col)) {
            ctx.fillRect(margin + col * cellSize, margin + row * cellSize, cellSize, cellSize);
          }
        }
      }
      return canvas.toDataURL("image/png");
    },
  };

  function qrcode(typeNumber, errorCorrectionLevel) {
    return new QRCode(typeNumber, QRErrorCorrectionLevel[errorCorrectionLevel] || QRErrorCorrectionLevel.M);
  }

  window.MSCQRCode = {
    toDataURL: function (text, size) {
      var payload = String(text || "");
      var length = payload.length;
      var typeNumber = 1;
      if (length > 180) {
        typeNumber = 10;
      } else if (length > 152) {
        typeNumber = 9;
      } else if (length > 122) {
        typeNumber = 8;
      } else if (length > 106) {
        typeNumber = 7;
      } else if (length > 84) {
        typeNumber = 6;
      } else if (length > 62) {
        typeNumber = 5;
      } else if (length > 42) {
        typeNumber = 4;
      } else if (length > 26) {
        typeNumber = 3;
      } else if (length > 14) {
        typeNumber = 2;
      }
      var qr = qrcode(typeNumber, "M");
      qr.addData(payload);
      qr.make();
      var margin = 8;
      var cell = Math.max(2, Math.floor((size || 256) / qr.getModuleCount()));
      return qr.createDataURL(cell, margin);
    },
  };
})();
