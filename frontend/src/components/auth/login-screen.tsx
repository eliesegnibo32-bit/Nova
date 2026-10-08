"use client";

import * as React from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { motion, MotionConfig, useReducedMotion } from "framer-motion";
import { toast } from "sonner";
import {
  Sparkles,
  MessageSquareText,
  Boxes,
  Truck,
  Loader2,
  Lock,
  Mail,
  ArrowRight,
  Store,
  CheckCircle2,
  User,
  Phone,
  KeyRound,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { NovaLogo } from "@/components/ui/nova-logo";
import { useAuthStore } from "@/stores/auth-store";
import { authApi, ApiError } from "@/lib/api";

const loginSchema = z.object({
  email: z.string().email("Adresse e-mail invalide"),
  password: z.string().min(6, "Mot de passe trop court (6 caractères min)"),
});

type LoginForm = z.infer<typeof loginSchema>;

const highlights = [
  {
    icon: MessageSquareText,
    title: "IA WhatsApp 24/7",
    description:
      "NOVA répond à vos clients, prend les commandes et confirme les livraisons, sans vous.",
  },
  {
    icon: Boxes,
    title: "Stock & catalogue centralisés",
    description:
      "Gérez produits, variantes et quantités en temps réel, sans rupture surprise.",
  },
  {
    icon: Truck,
    title: "Livraisons orchestrées",
    description:
      "Zones, livreurs et statuts de livraison pilotés depuis un seul tableau de bord.",
  },
];

export function LoginScreen() {
  const loginDemo = useAuthStore((s) => s.loginDemo);
  const setUser = useAuthStore((s) => s.setUser);
  const setShop = useAuthStore((s) => s.setShop);
  const [submitting, setSubmitting] = React.useState(false);
  const [registerOpen, setRegisterOpen] = React.useState(false);
  const [forgotOpen, setForgotOpen] = React.useState(false);
  const prefersReducedMotion = useReducedMotion();

  const form = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "" },
  });

  async function onSubmit(values: LoginForm) {
    setSubmitting(true);
    try {
      const res = await authApi.login(values.email, values.password);
      if (res?.user) {
        setUser(res.user);
        // Si l'utilisateur a déjà des boutiques, sélectionner la première automatiquement
        const shops = (res as any)?.shops || [];
        if (shops.length > 0) {
          const shopId = shops[0]?.shop_id || shops[0]?.id;
          if (shopId) {
            setShop(shopId);
          }
        }
        toast.success(`Bienvenue, ${res.user.fullName}`);
      }
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Connexion impossible";
      toast.error(message);
    } finally {
      setSubmitting(false);
    }
  }

  function handleDemo() {
    loginDemo();
    toast.success("Mode démo activé", {
      description: "Vous explorez NOVA avec des données mockées.",
    });
  }

  // === Motion variants ===
  const containerVariants = {
    hidden: {},
    visible: {
      transition: { staggerChildren: 0.1, delayChildren: 0.1 },
    },
  };

  const fadeUp = prefersReducedMotion
    ? {
        hidden: { opacity: 0 },
        visible: { opacity: 1, transition: { duration: 0.2 } },
      }
    : {
        hidden: { opacity: 0, y: 16 },
        visible: {
          opacity: 1,
          y: 0,
          transition: { type: "spring" as const, stiffness: 260, damping: 24 },
        },
      };

  // Noise texture as inline SVG (5% opacity overlay)
  const noiseStyle: React.CSSProperties = {
    backgroundImage:
      "url(\"data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='160' height='160'><filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.85' numOctaves='2' stitchTiles='stitch'/></filter><rect width='100%25' height='100%25' filter='url(%23n)'/></svg>\")",
  };

  return (
    <MotionConfig reducedMotion="user">
      <div className="grid min-h-screen w-full grid-cols-1 lg:grid-cols-[55fr_45fr]">
        {/* === Panneau marque (desktop, 55%) === */}
        <aside
          aria-label="Présentation de NOVA"
          className="nova-brand-gradient relative hidden flex-col justify-between overflow-hidden p-12 text-white lg:flex"
        >
          {/* Orbes flottants (emerald / amber / teal) */}
          {!prefersReducedMotion && (
            <>
              <motion.div
                aria-hidden
                className="pointer-events-none absolute -left-24 -top-24 size-80 rounded-full blur-3xl"
                style={{ background: "oklch(0.7 0.15 162 / 0.45)" }}
                animate={{
                  x: [0, 30, -20, 0],
                  y: [0, -40, 20, 0],
                  scale: [1, 1.1, 0.95, 1],
                }}
                transition={{
                  duration: 12,
                  repeat: Infinity,
                  repeatType: "reverse",
                  ease: "easeInOut",
                }}
              />
              <motion.div
                aria-hidden
                className="pointer-events-none absolute -right-24 top-1/3 size-96 rounded-full blur-3xl"
                style={{ background: "oklch(0.78 0.16 75 / 0.35)" }}
                animate={{
                  x: [0, -25, 15, 0],
                  y: [0, 35, -25, 0],
                  scale: [1, 0.9, 1.1, 1],
                }}
                transition={{
                  duration: 15,
                  repeat: Infinity,
                  repeatType: "reverse",
                  ease: "easeInOut",
                }}
              />
              <motion.div
                aria-hidden
                className="pointer-events-none absolute -bottom-32 left-1/4 size-72 rounded-full blur-3xl"
                style={{ background: "oklch(0.6 0.12 195 / 0.4)" }}
                animate={{
                  x: [0, 20, -30, 0],
                  y: [0, -30, 15, 0],
                  scale: [1, 1.15, 0.92, 1],
                }}
                transition={{
                  duration: 18,
                  repeat: Infinity,
                  repeatType: "reverse",
                  ease: "easeInOut",
                }}
              />
            </>
          )}

          {/* Bruit SVG (5% opacity) */}
          <div
            aria-hidden
            className="pointer-events-none absolute inset-0 opacity-5 mix-blend-overlay"
            style={noiseStyle}
          />

          {/* Logo */}
          <motion.div
            variants={fadeUp}
            initial="hidden"
            animate="visible"
            className="relative z-10"
          >
            <NovaLogo variant="white" withWordmark size={44} />
          </motion.div>

          {/* Contenu principal (badge + headline + paragraphe + highlights) */}
          <motion.div
            variants={containerVariants}
            initial="hidden"
            animate="visible"
            className="relative z-10 max-w-md space-y-8"
          >
            <motion.div variants={fadeUp} className="space-y-4">
              <div className="glass-soft inline-flex items-center gap-2 rounded-full px-3 py-1 text-xs font-medium text-white">
                <Sparkles
                  className="size-3.5"
                  style={{ color: "var(--amber-accent)" }}
                />
                Employé numérique de votre commerce
              </div>
              <h1 className="text-4xl font-bold leading-tight tracking-tight">
                Votre boutique ne dort jamais.
              </h1>
              <p className="text-lg text-white/85">
                NOVA répond à vos clients WhatsApp, prend les commandes, suit le
                stock et orchestre les livraisons à Abidjan.
              </p>
            </motion.div>

            <motion.ul
              variants={containerVariants}
              initial="hidden"
              animate="visible"
              className="space-y-4"
            >
              {highlights.map((h) => (
                <motion.li
                  key={h.title}
                  variants={fadeUp}
                  className="group flex items-start gap-3"
                >
                  <motion.div
                    whileHover={
                      prefersReducedMotion ? undefined : { scale: 1.1 }
                    }
                    transition={{ type: "spring", stiffness: 400, damping: 17 }}
                    className="glass-soft flex size-10 shrink-0 items-center justify-center rounded-lg text-white transition-shadow duration-300 group-hover:shadow-[0_0_20px_oklch(0.78_0.16_75_/_0.6)]"
                  >
                    <h.icon className="size-5" />
                  </motion.div>
                  <div>
                    <div className="font-semibold">{h.title}</div>
                    <div className="text-sm text-white/75">{h.description}</div>
                  </div>
                </motion.li>
              ))}
            </motion.ul>
          </motion.div>

          {/* Footer */}
          <motion.div
            variants={fadeUp}
            initial="hidden"
            animate="visible"
            transition={{ delay: 0.7 }}
            className="relative z-10 text-xs text-white/60"
          >
            © 2026 NOVA — Conçu à Abidjan pour les commerçants de Côte d'Ivoire.
          </motion.div>
        </aside>

        {/* === Panneau formulaire (45%) === */}
        <main
          aria-label="Connexion à NOVA"
          className="nova-form-gradient relative flex flex-col"
        >
          {/* En-tête mobile compact (gradient + logo + headline) */}
          <div className="nova-brand-gradient relative overflow-hidden lg:hidden">
            <div className="relative z-10 flex flex-col gap-3 p-6 text-white">
              <NovaLogo variant="white" withWordmark size={36} />
              <h1 className="text-2xl font-bold leading-tight tracking-tight">
                Votre boutique ne dort jamais.
              </h1>
              <p className="text-sm text-white/80">
                NOVA — l&apos;employé numérique de votre commerce à Abidjan.
              </p>
            </div>
          </div>

          <div className="flex flex-1 items-center justify-center p-6 sm:p-10">
            <motion.div
              initial={
                prefersReducedMotion
                  ? { opacity: 0 }
                  : { opacity: 0, scale: 0.95 }
              }
              animate={
                prefersReducedMotion ? { opacity: 1 } : { opacity: 1, scale: 1 }
              }
              transition={{ type: "spring", stiffness: 260, damping: 24 }}
              className="glass w-full max-w-md space-y-6 rounded-2xl p-8 shadow-2xl"
            >
              <div className="space-y-1.5">
                <h2 className="text-2xl font-bold tracking-tight">
                  Connexion à NOVA
                </h2>
                <p className="text-sm text-muted-foreground">
                  Accédez à votre tableau de bord commerçant.
                </p>
              </div>

              <form
                onSubmit={form.handleSubmit(onSubmit)}
                className="space-y-4"
                noValidate
              >
                {/* Email */}
                <div className="space-y-1.5">
                  <Label htmlFor="email">Adresse e-mail</Label>
                  <div className="group relative">
                    <Mail className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground transition-all duration-200 group-focus-within:-translate-x-1 group-focus-within:text-brand" />
                    <Input
                      id="email"
                      type="email"
                      autoComplete="email"
                      placeholder="vous@boutique.ci"
                      className="pl-9"
                      aria-label="Adresse e-mail"
                      {...form.register("email")}
                    />
                  </div>
                  {form.formState.errors.email && (
                    <p className="text-xs text-destructive" role="alert">
                      {form.formState.errors.email.message}
                    </p>
                  )}
                </div>

                {/* Mot de passe */}
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <Label htmlFor="password">Mot de passe</Label>
                    <button
                      type="button"
                      className="text-xs text-muted-foreground transition-colors hover:text-foreground hover:underline"
                      onClick={() => setForgotOpen(true)}
                    >
                      Mot de passe oublié ?
                    </button>
                  </div>
                  <div className="group relative">
                    <Lock className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground transition-all duration-200 group-focus-within:-translate-x-1 group-focus-within:text-brand" />
                    <Input
                      id="password"
                      type="password"
                      autoComplete="current-password"
                      placeholder="••••••••"
                      className="pl-9"
                      aria-label="Mot de passe"
                      {...form.register("password")}
                    />
                  </div>
                  {form.formState.errors.password && (
                    <p className="text-xs text-destructive" role="alert">
                      {form.formState.errors.password.message}
                    </p>
                  )}
                </div>

                {/* Bouton de soumission (hover scale + tap scale + shimmer sweep) */}
                <motion.div
                  className="relative"
                  initial="rest"
                  animate="rest"
                  whileHover={prefersReducedMotion ? undefined : "hover"}
                  whileTap={prefersReducedMotion ? undefined : "tap"}
                  variants={{
                    rest: { scale: 1 },
                    hover: prefersReducedMotion
                      ? { scale: 1 }
                      : { scale: 1.02 },
                    tap: prefersReducedMotion ? { scale: 1 } : { scale: 0.98 },
                  }}
                  transition={{ type: "spring", stiffness: 400, damping: 17 }}
                >
                  <Button
                    type="submit"
                    className="relative w-full overflow-hidden"
                    size="lg"
                    disabled={submitting}
                  >
                    {!prefersReducedMotion && !submitting && (
                      <motion.span
                        aria-hidden
                        variants={{
                          rest: { x: "-200%" },
                          hover: { x: "400%" },
                        }}
                        transition={{ duration: 0.9, ease: "easeInOut" }}
                        className="pointer-events-none absolute inset-y-0 left-0 w-1/2 skew-x-[-12deg] bg-gradient-to-r from-transparent via-white/40 to-transparent"
                      />
                    )}
                    {submitting ? (
                      <>
                        <Loader2 className="size-4 animate-spin" />
                        Connexion…
                      </>
                    ) : (
                      <>
                        Se connecter
                        <ArrowRight className="size-4" />
                      </>
                    )}
                  </Button>
                </motion.div>
              </form>

              {/* Séparateur "ou" avec animation draw-line */}
              <div
                className="relative flex items-center justify-center py-1"
                aria-hidden
              >
                <motion.div
                  className="absolute left-0 top-1/2 h-px w-[42%] origin-left bg-border"
                  initial={{ scaleX: 0 }}
                  animate={{ scaleX: 1 }}
                  transition={{ delay: 0.4, duration: 0.5, ease: "easeOut" }}
                />
                <span className="z-10 px-3 text-xs uppercase text-muted-foreground">
                  ou
                </span>
                <motion.div
                  className="absolute right-0 top-1/2 h-px w-[42%] origin-right bg-border"
                  initial={{ scaleX: 0 }}
                  animate={{ scaleX: 1 }}
                  transition={{ delay: 0.4, duration: 0.5, ease: "easeOut" }}
                />
              </div>

              {/* Bouton démo (Sparkles rotate on hover) */}
              <motion.div
                className="relative"
                initial="rest"
                animate="rest"
                whileHover={prefersReducedMotion ? undefined : "hover"}
                variants={{
                  rest: { scale: 1 },
                  hover: prefersReducedMotion ? { scale: 1 } : { scale: 1.01 },
                }}
                transition={{ type: "spring", stiffness: 400, damping: 17 }}
              >
                <Button
                  type="button"
                  variant="outline"
                  className="w-full"
                  size="lg"
                  onClick={handleDemo}
                >
                  <motion.span
                    className="inline-flex"
                    variants={{
                      rest: { rotate: 0 },
                      hover: prefersReducedMotion
                        ? { rotate: 0 }
                        : { rotate: 180 },
                    }}
                    transition={{ duration: 0.4 }}
                  >
                    <Sparkles className="size-4 text-brand" />
                  </motion.span>
                  Explorer en Mode démo
                </Button>
              </motion.div>

              {/* Identifiants démo */}
              <div className="rounded-lg border border-dashed bg-muted/40 p-3 text-xs text-muted-foreground">
                <span className="font-medium text-foreground">
                  Identifiants démo :
                </span>{" "}
                admin@nova.ci / Admin1234! — ou utilisez le Mode démo pour
                explorer avec des données mockées.
              </div>

              <p className="text-center text-xs text-muted-foreground">
                Pas encore de compte ?{" "}
                <button
                  type="button"
                  className="font-medium text-brand hover:underline"
                  onClick={() => setRegisterOpen(true)}
                >
                  Créer un compte
                </button>
              </p>
            </motion.div>
          </div>
        </main>

        {/* Dialog: Inscription */}
        <RegisterDialog
          open={registerOpen}
          onOpenChange={setRegisterOpen}
          setUser={setUser}
        />

        {/* Dialog: Mot de passe oublié */}
        <ForgotPasswordDialog open={forgotOpen} onOpenChange={setForgotOpen} />
      </div>
    </MotionConfig>
  );
}

// ====== Dialog Inscription (avec page de succès) ======

function RegisterDialog({
  open,
  onOpenChange,
  setUser,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  setUser: (u: any) => void;
}) {
  const [submitting, setSubmitting] = React.useState(false);
  const [success, setSuccess] = React.useState(false);
  const [createdUser, setCreatedUser] = React.useState<any>(null);
  const [form, setForm] = React.useState({
    fullName: "",
    shopName: "",
    email: "",
    phone: "+225",
    password: "",
    confirmPassword: "",
  });

  function reset() {
    setForm({
      fullName: "",
      shopName: "",
      email: "",
      phone: "+225",
      password: "",
      confirmPassword: "",
    });
    setSuccess(false);
    setCreatedUser(null);
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (form.password !== form.confirmPassword) {
      toast.error("Les mots de passe ne correspondent pas.");
      return;
    }
    if (form.password.length < 8) {
      toast.error("Mot de passe trop court (8 caractères min).");
      return;
    }
    setSubmitting(true);
    authApi
      .register({
        email: form.email,
        password: form.password,
        full_name: form.fullName,
        phone: form.phone,
      })
      .then((res) => {
        if (res?.user) {
          setUser(res.user);
          setCreatedUser(res.user);
          setSuccess(true);
          toast.success("Compte créé avec succès !");
        }
      })
      .catch((err) => {
        const msg =
          err instanceof ApiError ? err.message : "Inscription impossible";
        toast.error(msg);
      })
      .finally(() => setSubmitting(false));
  }

  function handleContinue() {
    onOpenChange(false);
    // L'onboarding s'affichera automatiquement car currentShopId = null
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) reset();
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-[520px]">
        {success ? (
          // ====== PAGE DE SUCCÈS ======
          <div className="flex flex-col items-center py-6 text-center">
            <motion.div
              initial={{ scale: 0 }}
              animate={{ scale: 1 }}
              transition={{ type: "spring", stiffness: 200, damping: 15 }}
              className="mb-4 flex size-16 items-center justify-center rounded-full bg-emerald-100 dark:bg-emerald-900"
            >
              <CheckCircle2 className="size-9 text-emerald-600 dark:text-emerald-400" />
            </motion.div>
            <DialogTitle className="text-xl">
              Inscription terminée !
            </DialogTitle>
            <DialogDescription className="mt-2 text-center">
              Votre compte NOVA a été créé avec succès. Bienvenue{" "}
              <strong>{createdUser?.fullName || form.fullName}</strong> !
            </DialogDescription>
            <div className="mt-6 w-full space-y-3 rounded-lg border bg-muted/30 p-4 text-left">
              <div className="flex items-center gap-2 text-sm">
                <CheckCircle2 className="size-4 text-emerald-500" />
                <span>
                  Compte créé : <strong>{form.email}</strong>
                </span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <CheckCircle2 className="size-4 text-emerald-500" />
                <span>
                  Téléphone : <strong>{form.phone}</strong>
                </span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <Store className="size-4 text-brand" />
                <span>Prochaine étape : créer votre boutique</span>
              </div>
            </div>
            <Button onClick={handleContinue} size="lg" className="mt-6 w-full">
              Continuer
              <ArrowRight className="size-4" />
            </Button>
          </div>
        ) : (
          // ====== FORMULAIRE D'INSCRIPTION ======
          <>
            <DialogHeader>
              <DialogTitle>Créer votre compte NOVA</DialogTitle>
              <DialogDescription>
                Inscrivez-vous pour gérer votre boutique avec l'assistant IA
                WhatsApp.
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit} className="space-y-4">
              {/* Section: Vos informations */}
              <div className="space-y-3">
                <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                  <User className="size-4" />
                  Vos informations
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="reg-fullname">Nom et prénom *</Label>
                  <div className="relative">
                    <User className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      id="reg-fullname"
                      required
                      placeholder="Awa Traoré"
                      className="pl-9"
                      value={form.fullName}
                      onChange={(e) =>
                        setForm({ ...form, fullName: e.target.value })
                      }
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="reg-shopname">Nom de votre boutique *</Label>
                  <div className="relative">
                    <Store className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      id="reg-shopname"
                      required
                      placeholder="Boutique Abidjan Mode"
                      className="pl-9"
                      value={form.shopName}
                      onChange={(e) =>
                        setForm({ ...form, shopName: e.target.value })
                      }
                    />
                  </div>
                </div>
              </div>

              {/* Section: Contact */}
              <div className="space-y-3 border-t pt-3">
                <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                  <Phone className="size-4" />
                  Coordonnées
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="reg-email">E-mail *</Label>
                  <div className="relative">
                    <Mail className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      id="reg-email"
                      required
                      type="email"
                      placeholder="vous@boutique.ci"
                      className="pl-9"
                      value={form.email}
                      onChange={(e) =>
                        setForm({ ...form, email: e.target.value })
                      }
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="reg-phone">Téléphone WhatsApp *</Label>
                  <div className="relative">
                    <Phone className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      id="reg-phone"
                      required
                      placeholder="+225 07 00 00 00 00"
                      className="pl-9"
                      value={form.phone}
                      onChange={(e) =>
                        setForm({ ...form, phone: e.target.value })
                      }
                    />
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Ce numéro sera utilisé pour vous contacter et configurer
                    WhatsApp Business.
                  </p>
                </div>
              </div>

              {/* Section: Sécurité */}
              <div className="space-y-3 border-t pt-3">
                <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                  <Lock className="size-4" />
                  Sécurité
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="reg-password">
                    Mot de passe * (8 caractères min)
                  </Label>
                  <div className="relative">
                    <Lock className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      id="reg-password"
                      required
                      type="password"
                      placeholder="••••••••"
                      className="pl-9"
                      value={form.password}
                      onChange={(e) =>
                        setForm({ ...form, password: e.target.value })
                      }
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="reg-confirm">
                    Confirmer le mot de passe *
                  </Label>
                  <div className="relative">
                    <Lock className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      id="reg-confirm"
                      required
                      type="password"
                      placeholder="••••••••"
                      className="pl-9"
                      value={form.confirmPassword}
                      onChange={(e) =>
                        setForm({ ...form, confirmPassword: e.target.value })
                      }
                    />
                  </div>
                </div>
              </div>

              <Button
                type="submit"
                className="w-full"
                size="lg"
                disabled={submitting}
              >
                {submitting ? (
                  <>
                    <Loader2 className="size-4 animate-spin" /> Création du
                    compte…
                  </>
                ) : (
                  <>
                    Créer mon compte
                    <ArrowRight className="size-4" />
                  </>
                )}
              </Button>
              <p className="text-center text-xs text-muted-foreground">
                En créant un compte, vous acceptez les conditions d'utilisation
                de NOVA.
              </p>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ====== Dialog Mot de passe oublié ======

function ForgotPasswordDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const [step, setStep] = React.useState<1 | 2>(1);
  const [email, setEmail] = React.useState("");
  const [token, setToken] = React.useState("");
  const [newPassword, setNewPassword] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);

  async function handleRequest(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      const res = await authApi.requestPasswordReset(email);
      const devToken = (res as any)?.dev_token;
      if (devToken) {
        setToken(devToken);
        toast.success("Code de réinitialisation généré", {
          description: `Code (mode dev): ${devToken}`,
        });
      } else {
        toast.success("Code envoyé", {
          description:
            "Un code de réinitialisation a été envoyé par e-mail/WhatsApp/SMS.",
        });
      }
      setStep(2);
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Erreur";
      toast.error(msg);
    } finally {
      setSubmitting(false);
    }
  }

  async function handleConfirm(e: React.FormEvent) {
    e.preventDefault();
    if (newPassword.length < 8) {
      toast.error("Mot de passe trop court (8 caractères min).");
      return;
    }
    setSubmitting(true);
    try {
      await authApi.confirmPasswordReset(token, newPassword);
      toast.success("Mot de passe réinitialisé", {
        description: "Vous pouvez maintenant vous connecter.",
      });
      onOpenChange(false);
      setStep(1);
      setEmail("");
      setToken("");
      setNewPassword("");
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Erreur";
      toast.error(msg);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) {
          setStep(1);
          setEmail("");
          setToken("");
          setNewPassword("");
        }
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Mot de passe oublié</DialogTitle>
          <DialogDescription>
            {step === 1
              ? "Entrez votre e-mail pour recevoir un code de réinitialisation."
              : "Entrez le code reçu et votre nouveau mot de passe."}
          </DialogDescription>
        </DialogHeader>
        {step === 1 ? (
          <form onSubmit={handleRequest} className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="fp-email">E-mail</Label>
              <div className="relative">
                <Mail className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="fp-email"
                  required
                  type="email"
                  placeholder="vous@boutique.ci"
                  className="pl-9"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>
            </div>
            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="size-4 animate-spin" /> Envoi…
                </>
              ) : (
                "Envoyer le code"
              )}
            </Button>
          </form>
        ) : (
          <form onSubmit={handleConfirm} className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="fp-code">Code de vérification</Label>
              <div className="relative">
                <KeyRound className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="fp-code"
                  required
                  placeholder="Code à 6 chiffres"
                  className="pl-9"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="fp-newpass">Nouveau mot de passe</Label>
              <Input
                id="fp-newpass"
                required
                type="password"
                placeholder="••••••••"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
              />
            </div>
            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="size-4 animate-spin" /> Réinitialisation…
                </>
              ) : (
                "Réinitialiser"
              )}
            </Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
