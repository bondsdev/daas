import { useAuthStore } from "~/stores/auth";

export default defineNuxtRouteMiddleware((to) => {
  // Auth is localStorage-based — only the client can read it.
  // Running redirects on the server causes a layout switch (auth→default)
  // on the client that leaves auth.vue's wrapper permanently wrapping the
  // default layout content, breaking the dashboard layout.
  if (import.meta.server) return;

  const store = useAuthStore();
  // Hydrate from localStorage on first navigation (client-side)
  store.initFromStorage();

  const publicPaths = [
    "/auth/login",
    "/auth/register",
    "/auth/forgot-password",
    "/auth/update-password",
    "/t/",
  ];
  const isPublic = publicPaths.some((p) => to.path.startsWith(p));

  if (!store.isAuthenticated && !isPublic) {
    return navigateTo("/auth/login");
  }

  if (store.isAuthenticated && to.path.startsWith("/auth")) {
    return navigateTo(store.isRider ? "/my-deliveries" : "/dashboard");
  }

  // Riders only have their own mobile surface — no owner/employee dashboard
  // route restriction exists yet beyond this, so keep this minimal.
  if (store.isRider && !isPublic && !to.path.startsWith("/my-deliveries")) {
    return navigateTo("/my-deliveries");
  }
});
