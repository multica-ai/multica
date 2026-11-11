/*
 * The logo wall: companies and universities whose people have signed up for
 * Multica, from email domains. One list for every locale. Each logo is the
 * organization's official artwork, flattened to a one-colour alpha mask in
 * `public/images/logos` so the wall can tint it; `ratio` is the mask's
 * width / height.
 */

export type CustomerLogo = { name: string; logo: string; ratio: number };

export const CUSTOMER_LOGOS: CustomerLogo[] = [
  { name: "NVIDIA", logo: "nvidia", ratio: 5.42 },
  { name: "ByteDance", logo: "bytedance", ratio: 5.77 },
  { name: "Microsoft", logo: "microsoft", ratio: 4.69 },
  { name: "Samsung", logo: "samsung", ratio: 6.52 },
  { name: "Google", logo: "google", ratio: 3.04 },
  { name: "Tencent", logo: "tencent", ratio: 7.79 },
  { name: "Amazon", logo: "amazon", ratio: 3.32 },
  { name: "Rakuten", logo: "rakuten", ratio: 5.88 },
  { name: "Alibaba", logo: "alibaba", ratio: 7.79 },
  { name: "UC Berkeley", logo: "berkeley", ratio: 5.32 },
  { name: "Xiaomi", logo: "xiaomi", ratio: 3.75 },
  { name: "SAP", logo: "sap", ratio: 2.02 },
  { name: "Meituan", logo: "meituan", ratio: 2.83 },
  { name: "Zoom", logo: "zoom", ratio: 4.44 },
  { name: "University of Oxford", logo: "oxford", ratio: 3.25 },
  { name: "Shopee", logo: "shopee", ratio: 3.13 },
  { name: "adidas", logo: "adidas", ratio: 1.48 },
  { name: "Tsinghua University", logo: "tsinghua", ratio: 1.0 },
  { name: "DoorDash", logo: "doordash", ratio: 8.57 },
  { name: "JD.com", logo: "jd", ratio: 3.06 },
];
