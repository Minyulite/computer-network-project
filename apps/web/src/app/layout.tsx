import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Commonroom — your conversations",
  description: "Private conversations and shared groups, across your devices.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
