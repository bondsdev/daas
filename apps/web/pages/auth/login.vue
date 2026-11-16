<script setup lang="ts">
import { Eye, EyeOff, Loader2 } from "lucide-vue-next";
import { useAuthStore } from "~/stores/auth";

definePageMeta({
  layout: "auth",
  middleware: "auth",
});

const store = useAuthStore();
const router = useRouter();

const email = ref("");
const password = ref("");
const showPassword = ref(false);
const loading = ref(false);
const errorMessage = ref("");

const emailError = ref("");
const passwordError = ref("");

function validateEmail(val: string): boolean {
  const re = /^[^\s@]+@[^\s@][^\s.@]*\.[^\s@]+$/;
  return re.test(val);
}

function validateForm(): boolean {
  let valid = true;
  emailError.value = "";
  passwordError.value = "";

  if (!email.value) {
    emailError.value = "El email es requerido";
    valid = false;
  } else if (!validateEmail(email.value)) {
    emailError.value = "Ingresá un email válido";
    valid = false;
  }

  if (!password.value) {
    passwordError.value = "La contraseña es requerida";
    valid = false;
  } else if (password.value.length < 8) {
    passwordError.value = "La contraseña debe tener al menos 8 caracteres";
    valid = false;
  }

  return valid;
}

async function handleSubmit() {
  if (!validateForm()) return;

  loading.value = true;
  errorMessage.value = "";

  try {
    await store.login(email.value, password.value);
    await navigateTo(store.isRider ? "/my-deliveries" : "/dashboard");
  } catch (err: unknown) {
    const apiError = err as { data?: { detail?: string }; message?: string };
    errorMessage.value =
      apiError?.data?.detail ??
      apiError?.message ??
      "Error al iniciar sesión. Intentá nuevamente.";
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="w-full max-w-md">
    <!-- Brand -->
    <div class="text-center mb-8">
      <h1 class="text-4xl font-bold font-heading text-primary">DaaS</h1>
      <p class="text-sm text-muted-foreground mt-1 font-body">
        Gestión de inventario empresarial
      </p>
    </div>

    <!-- Card -->
    <div class="rounded-xl border bg-card border-border shadow-md">
      <div class="px-6 pt-6 pb-2">
        <h2 class="text-2xl font-bold font-heading text-foreground">
          Iniciar sesión
        </h2>
        <p class="text-sm text-muted-foreground mt-1">
          Accedé a tu cuenta de empresa
        </p>
      </div>

      <form
        class="px-6 pb-6 pt-4 space-y-4"
        novalidate
        @submit.prevent="handleSubmit"
      >
        <!-- Email -->
        <div>
          <label
            for="email"
            class="block text-sm font-semibold text-foreground mb-1.5"
          >
            Email
          </label>
          <input
            id="email"
            v-model="email"
            type="email"
            placeholder="tu@empresa.com"
            autocomplete="email"
            :disabled="loading"
            :class="[
              'w-full h-11 px-4 border rounded-lg text-base transition-all duration-200',
              'focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20',
              'placeholder:text-muted-foreground',
              emailError ? 'border-red-400' : 'border-input',
              loading && 'opacity-50 cursor-not-allowed',
            ]"
          />
          <p v-if="emailError" class="text-xs text-red-500 mt-1">
            {{ emailError }}
          </p>
        </div>

        <!-- Password -->
        <div>
          <label
            for="password"
            class="block text-sm font-semibold text-foreground mb-1.5"
          >
            Contraseña
          </label>
          <div class="relative">
            <input
              id="password"
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              placeholder="Mínimo 8 caracteres"
              autocomplete="current-password"
              :disabled="loading"
              :class="[
                'w-full h-11 px-4 pr-11 border rounded-lg text-base transition-all duration-200',
                'focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20',
                'placeholder:text-muted-foreground',
                passwordError ? 'border-red-400' : 'border-input',
                loading && 'opacity-50 cursor-not-allowed',
              ]"
            />
            <button
              type="button"
              class="absolute inset-y-0 right-0 flex items-center px-3 text-muted-foreground hover:text-muted-foreground transition-colors duration-200 cursor-pointer"
              :aria-label="
                showPassword ? 'Ocultar contraseña' : 'Mostrar contraseña'
              "
              @click="showPassword = !showPassword"
            >
              <component :is="showPassword ? EyeOff : Eye" class="w-4 h-4" />
            </button>
          </div>
          <p v-if="passwordError" class="text-xs text-red-500 mt-1">
            {{ passwordError }}
          </p>
          <div class="mt-1.5 text-right">
            <NuxtLink
              to="/auth/forgot-password"
              class="text-xs font-semibold text-primary hover:underline cursor-pointer"
            >
              ¿Olvidaste tu contraseña?
            </NuxtLink>
          </div>
        </div>

        <!-- Error message (API) -->
        <div
          v-if="errorMessage"
          class="rounded-lg bg-red-50 border border-red-200 px-4 py-3"
        >
          <p class="text-sm text-red-600">
            {{ errorMessage }}
          </p>
        </div>

        <!-- Submit button -->
        <button
          type="submit"
          :disabled="loading"
          class="w-full h-12 flex items-center justify-center gap-2 bg-primary text-white rounded-lg font-semibold text-base transition-all duration-200 cursor-pointer hover:opacity-90 focus-visible:outline-2 focus-visible:outline-primary disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Loader2 v-if="loading" class="w-5 h-5 animate-spin" />
          <span>{{ loading ? "Ingresando..." : "Ingresar" }}</span>
        </button>

        <!-- Register link -->
        <p class="text-center text-sm text-muted-foreground">
          No tenés cuenta?
          <NuxtLink
            to="/auth/register"
            class="text-primary font-semibold hover:underline cursor-pointer transition-all duration-200"
          >
            Registrate
          </NuxtLink>
        </p>
      </form>
    </div>
  </div>
</template>
