import type { NextConfig } from "next";

const API_SERVER = process.env.NEXT_PUBLIC_API_URL || "http://localhost:9090";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${API_SERVER}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
