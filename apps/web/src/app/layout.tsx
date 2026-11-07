import type { Metadata } from "next";
import { Instrument_Sans, Newsreader, Poppins } from "next/font/google";
import "./globals.css";
import "./chat.css";

// Self-hosted at build time, so the app needs no font requests at runtime.
const sans = Instrument_Sans({ subsets: ["latin"], variable: "--font-sans", display: "swap" });
const serif = Newsreader({ subsets: ["latin"], variable: "--font-serif", display: "swap", weight: ["400", "500"] });

// The chat workspace uses Poppins, as in its design; sign-in keeps the other two.
const poppins = Poppins({ subsets: ["latin"], variable: "--font-poppins", display: "swap", weight: ["400", "500", "600"] });

export const metadata: Metadata = {
  title: "Commonroom",
  description: "Private conversations and shared groups, across your devices.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${sans.variable} ${serif.variable} ${poppins.variable}`}>
      <body>{children}</body>
    </html>
  );
}
