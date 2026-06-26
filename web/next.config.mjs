/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  // Emit a self-contained server bundle (.next/standalone) so the container
  // ships only the traced runtime deps instead of all of node_modules. See
  // Dockerfile.web (P11).
  output: "standalone",
};

export default nextConfig;
