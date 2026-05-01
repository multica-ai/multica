import type { MetadataRoute } from "next";

export default function robots(): MetadataRoute.Robots {
  const baseUrl = "https://forge.asymbl.app";

  return {
    rules: [
      {
        userAgent: "*",
        disallow: ["/"],
      },
    ],
    sitemap: [`${baseUrl}/sitemap.xml`],
  };
}
