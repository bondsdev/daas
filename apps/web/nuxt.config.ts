// NUXT_CAPACITOR=true → build estático para empaquetar con Capacitor (iOS/Android)
// Sin esa variable → SSR normal para el deploy web
const isCapacitorBuild = process.env.NUXT_CAPACITOR === "true";

export default defineNuxtConfig({
  devtools: { enabled: !isCapacitorBuild },
  ssr: !isCapacitorBuild, // SPA puro para Capacitor, SSR para web
  app: {
    head: {
      title: "Gestión de inventario",
      titleTemplate: "%s · DaaS",
      meta: [
        {
          name: "description",
          content:
            "Gestión de inventario multi-país con facturación fiscal y no fiscal.",
        },
        { name: "theme-color", content: "#7C3AED" },
      ],
      link: [
        { rel: "icon", type: "image/svg+xml", href: "/favicon.svg" },
        {
          rel: "icon",
          type: "image/png",
          sizes: "32x32",
          href: "/favicon-32x32.png",
        },
        {
          rel: "icon",
          type: "image/png",
          sizes: "16x16",
          href: "/favicon-16x16.png",
        },
        { rel: "icon", type: "image/x-icon", href: "/favicon.ico" },
        {
          rel: "apple-touch-icon",
          sizes: "180x180",
          href: "/apple-touch-icon.png",
        },
      ],
    },
  },
  modules: [
    "@pinia/nuxt",
    "@nuxtjs/tailwindcss",
    "shadcn-nuxt",
    "@nuxt/eslint",
    ...(isCapacitorBuild ? [] : ["@vite-pwa/nuxt" as const]),
  ],
  pwa: isCapacitorBuild
    ? undefined
    : {
        registerType: "autoUpdate",
        manifest: {
          name: "DaaS — Gestión de inventario",
          short_name: "DaaS",
          description:
            "Gestión de inventario multi-país con facturación fiscal y no fiscal.",
          theme_color: "#7C3AED",
          background_color: "#FAF5FF",
          display: "standalone",
          start_url: "/dashboard",
          icons: [
            {
              src: "/android-chrome-192x192.png",
              sizes: "192x192",
              type: "image/png",
            },
            {
              src: "/android-chrome-512x512.png",
              sizes: "512x512",
              type: "image/png",
            },
          ],
        },
        workbox: {
          navigateFallback: "/dashboard",
          globPatterns: ["**/*.{js,css,html,png,svg,ico}"],
        },
        devOptions: { enabled: false },
      },
  shadcn: {
    prefix: "",
    componentDir: "./components/ui",
  },
  css: ["~/assets/css/main.css"],
  typescript: {
    strict: true,
    typeCheck: false,
  },
  eslint: {
    config: {
      standalone: false,
    },
  },
  runtimeConfig: {
    public: {
      apiBase: process.env.NUXT_PUBLIC_API_BASE || "http://localhost:8080",
    },
  },
  // Rutas admin (detrás de login, sin valor SEO) → render client-only.
  // En cold load el server NO debe pintar el shell del dashboard: el auth
  // middleware es client-only (token en localStorage) y redirige a /auth/login
  // recién al hidratar. Sin esto, el primer paint muestra el layout default
  // mal posicionado hasta recargar. El storefront /t/** y /auth/** siguen con SSR.
  routeRules: {
    "/": { ssr: false },
    "/dashboard/**": { ssr: false },
    "/products/**": { ssr: false },
    "/inventory/**": { ssr: false },
    "/sales-orders/**": { ssr: false },
    "/shop-orders/**": { ssr: false },
    "/invoices/**": { ssr: false },
    "/suppliers/**": { ssr: false },
    "/reports/**": { ssr: false },
    "/settings/**": { ssr: false },
  },
  // Rutas a pre-renderizar cuando NUXT_CAPACITOR=true
  ...(isCapacitorBuild && {
    nitro: {
      prerender: {
        routes: ["/"],
        crawlLinks: true,
      },
    },
  }),
});
