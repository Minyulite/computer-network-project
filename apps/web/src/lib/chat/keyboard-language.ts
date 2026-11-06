const enToTh: Record<string, string> = {
  "`": "_", "~": "%", "1": "า", "!": "+", "2": "/", "@": "๑", "3": "-", "#": "๒",
  "4": "ภ", "$": "๓", "5": "ถ", "%": "๔", "6": "ุ", "^": "ู", "7": "ึ", "&": "๕",
  "8": "ค", "*": "๖", "9": "ต", "(": "๗", "0": "จ", ")": "๘", "-": "ข", "_": "๘",
  "=": "ช", "+": "๙", "q": "ๆ", "Q": "0", "w": "ไ", "W": "\"", "e": "ำ", "E": "ฎ", "r": "พ", "R": "ฑ",
  "t": "ะ", "T": "ธ", "y": "ั", "Y": "ํ", "u": "ี", "U": "๊", "i": "ร", "I": "ณ", "o": "น", "O": "ฯ", "p": "ย", "P": "ญ",
  "[": "บ", "{": "ฐ", "]": "ล", "}": ",", "\\": "ฃ", "|": "ฅ", "a": "ฟ", "A": "ฤ", "s": "ห", "S": "ฆ",
  "d": "ก", "D": "ฏ", "f": "ด", "F": "โ", "g": "เ", "G": "ฌ", "h": "้", "H": "็", "j": "่", "J": "๋", "k": "า", "K": "ษ",
  "l": "ส", "L": "ศ", ";": "ว", ":": "ซ", "'": "ง", "\"": ".", "z": "ผ", "Z": "(", "x": "ป", "X": ")",
  "c": "แ", "C": "ฉ", "v": "อ", "V": "ฮ", "b": "ิ", "B": "ฺ", "n": "ื", "N": "์", "m": "ท", "M": "?",
  ",": "ม", "<": "ฒ", ".": "ใ", ">": "ฬ", "/": "ฝ", "?": "ฦ",
};

const thToEn = Object.fromEntries(Object.entries(enToTh).map(([key, value]) => [value, key]));

export function convertKeyboardLanguage(value: string): string {
  const hasEnglishLetters = /[a-z]/i.test(value);
  const hasThaiCharacters = /[\u0E00-\u0E7F]/.test(value);
  const toThai = hasEnglishLetters || (!hasThaiCharacters && [...value].some(char => char in enToTh));
  const mapping = toThai ? enToTh : thToEn;
  return [...value].map(char => mapping[char] ?? char).join("");
}
