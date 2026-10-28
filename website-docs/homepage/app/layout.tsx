import type { Metadata } from "next";
import "./globals.css";
import { themeInitializationScript } from "./theme";

export const metadata: Metadata = {
  title: "AethCraft — 帮你找到答案，并将知识付诸实践",
  description: "AethCraft 企业知识框架，集 RAG 问答、Agent 推理与自动 Wiki 于一体，支持多模态检索和私有化部署。",
  openGraph: { title: "AethCraft", images: ["/docs/_home/brand/og.png"] },
  // Reuse the documentation favicon so the homepage adds nothing at the site root.
  icons: { icon: "/docs/favicon.ico" },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" className="h-full antialiased" suppressHydrationWarning>
      <head><script dangerouslySetInnerHTML={{ __html: themeInitializationScript }} /></head>
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  );
}
