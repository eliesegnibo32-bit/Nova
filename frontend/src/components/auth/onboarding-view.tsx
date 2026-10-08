"use client";

import * as React from "react";
import { motion } from "framer-motion";
import { toast } from "sonner";
import {
  Loader2,
  Plus,
  Store,
  Package,
  Truck,
  CreditCard,
  MessageSquareText,
  CheckCircle2,
  ArrowRight,
  Sparkles,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { NovaLogo } from "@/components/ui/nova-logo";
import { useAuthStore } from "@/stores/auth-store";
import { useUIStore } from "@/stores/ui-store";
import { shopsApi, ApiError } from "@/lib/api";

const PAYMENT_MODES = [
  { id: "cash", label: "Espèces" },
  { id: "orange_money", label: "Orange Money" },
  { id: "mtn_momo", label: "MTN MoMo" },
  { id: "wave", label: "Wave" },
];

const ONBOARDING_STEPS = [
  { icon: Store, title: "Créer votre boutique", description: "Nom, contact, adresse" },
  { icon: Package, title: "Ajouter vos produits", description: "Articles, plats, prix" },
  { icon: Truck, title: "Configurer la livraison", description: "Zones et tarifs" },
  { icon: CreditCard, title: "Configurer le paiement", description: "Modes et délais" },
  { icon: MessageSquareText, title: "Activer WhatsApp", description: "Connecter NOVA" },
];

export function OnboardingView() {
  const user = useAuthStore((s) => s.user);
  const setShop = useAuthStore((s) => s.setShop);
  const setCurrentView = useUIStore((s) => s.setCurrentView);
  const [step, setStep] = React.useState<"form" | "success">("form");
  const [submitting, setSubmitting] = React.useState(false);

  const [form, setForm] = React.useState({
    name: "",
    slug: "",
    phone: user?.phone || "+225",
    whatsapp_number: user?.phone || "+225",
    address: "",
    commune: "",
    description: "",
    categories: "",
    accepted_payment_modes: ["cash"] as string[],
  });

  // Auto-generate slug from name
  React.useEffect(() => {
    if (form.name) {
      setForm((prev) => ({
        ...prev,
        slug: prev.name
          .toLowerCase()
          .normalize("NFD")
          .replace(/[\u0300-\u036f]/g, "")
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-+|-+$/g, ""),
      }));
    }
  }, [form.name]);

  function toggleMode(mode: string) {
    setForm((prev) => ({
      ...prev,
      accepted_payment_modes: prev.accepted_payment_modes.includes(mode)
        ? prev.accepted_payment_modes.filter((m) => m !== mode)
        : [...prev.accepted_payment_modes, mode],
    }));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.name || !form.slug || !form.phone) {
      toast.error("Veuillez remplir au moins le nom et le téléphone.");
      return;
    }
    setSubmitting(true);
    try {
      const categories = form.categories
        .split(",")
        .map((c) => c.trim())
        .filter(Boolean);

      const res = (await shopsApi.create({
        name: form.name,
        slug: form.slug,
        phone: form.phone,
        whatsapp_number: form.whatsapp_number,
        address: form.address,
        commune: form.commune,
        description: form.description,
        categories,
        accepted_payment_modes: form.accepted_payment_modes,
      } as any)) as any;

      const shopId = res?.shop?.id || res?.id;
      if (shopId) {
        setShop(shopId);
        setStep("success");
        toast.success("Boutique créée avec succès !");
      }
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Création impossible";
      // If the user is not admin, they can't create a shop directly
      if (msg.includes("admin") || msg.includes("forbidden") || msg.includes("403")) {
        toast.error("Création de boutique réservée aux admins", {
          description: "Contactez l'équipe NOVA pour créer votre boutique, ou utilisez le Mode démo pour explorer.",
        });
      } else {
        toast.error(msg);
      }
    } finally {
      setSubmitting(false);
    }
  }

  function handleGoToDashboard() {
    setCurrentView("home");
  }

  function handleDemo() {
    useAuthStore.getState().loginDemo();
    setCurrentView("home");
  }

  if (step === "success") {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-emerald-50 to-amber-50 p-6 dark:from-emerald-950 dark:to-slate-950">
        <motion.div
          initial={{ opacity: 0, scale: 0.9 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ type: "spring", stiffness: 260, damping: 24 }}
          className="w-full max-w-lg"
        >
          <Card className="glass p-8 text-center sm:p-12">
            <motion.div
              initial={{ scale: 0 }}
              animate={{ scale: 1 }}
              transition={{ delay: 0.2, type: "spring", stiffness: 200 }}
              className="mx-auto mb-6 flex size-20 items-center justify-center rounded-full bg-emerald-100 dark:bg-emerald-900"
            >
              <CheckCircle2 className="size-10 text-emerald-600 dark:text-emerald-400" />
            </motion.div>
            <h2 className="mb-2 text-2xl font-bold">Boutique créée ! 🎉</h2>
            <p className="mb-8 text-muted-foreground">
              Votre boutique <strong>{form.name}</strong> est prête. Voici les prochaines étapes pour activer NOVA.
            </p>

            <div className="mb-8 space-y-3 text-left">
              {ONBOARDING_STEPS.map((s, i) => {
                const done = i === 0;
                return (
                  <motion.div
                    key={s.title}
                    initial={{ opacity: 0, x: -20 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: 0.3 + i * 0.1 }}
                    className={`flex items-center gap-3 rounded-lg border p-3 ${done ? "border-emerald-200 bg-emerald-50 dark:border-emerald-800 dark:bg-emerald-950" : "border-border"}`}
                  >
                    <div className={`flex size-9 shrink-0 items-center justify-center rounded-lg ${done ? "bg-emerald-500 text-white" : "bg-muted"}`}>
                      {done ? <CheckCircle2 className="size-5" /> : <s.icon className="size-5 text-muted-foreground" />}
                    </div>
                    <div className="flex-1">
                      <div className="text-sm font-medium">{s.title}</div>
                      <div className="text-xs text-muted-foreground">{s.description}</div>
                    </div>
                    {done && <Badge className="bg-emerald-500">Fait</Badge>}
                  </motion.div>
                );
              })}
            </div>

            <Button onClick={handleGoToDashboard} size="lg" className="w-full">
              Aller au tableau de bord
              <ArrowRight className="size-4" />
            </Button>
          </Card>
        </motion.div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-emerald-50 via-white to-amber-50 dark:from-emerald-950 dark:via-slate-950 dark:to-amber-950">
      <div className="mx-auto max-w-2xl px-4 py-8 sm:px-6 sm:py-12">
        {/* Header */}
        <motion.div
          initial={{ opacity: 0, y: -10 }}
          animate={{ opacity: 1, y: 0 }}
          className="mb-8 flex items-center justify-between"
        >
          <NovaLogo withWordmark size={36} />
          <Button variant="ghost" size="sm" onClick={handleDemo}>
            <Sparkles className="size-4 text-brand" />
            Mode démo
          </Button>
        </motion.div>

        {/* Titre */}
        <motion.div
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 0.1 }}
          className="mb-8 text-center"
        >
          <h1 className="mb-2 text-3xl font-bold tracking-tight">
            Bienvenue, {user?.fullName?.split(" ")[0] || "commerçant"} 👋
          </h1>
          <p className="text-muted-foreground">
            Votre compte est créé. Configurons votre boutique pour activer NOVA.
          </p>
        </motion.div>

        {/* Formulaire */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 0.2 }}
        >
          <Card className="glass p-6 sm:p-8">
            <form onSubmit={handleSubmit} className="space-y-6">
              {/* Section: Informations générales */}
              <div className="space-y-4">
                <div className="flex items-center gap-2">
                  <Store className="size-5 text-brand" />
                  <h3 className="text-sm font-semibold">Informations de la boutique</h3>
                </div>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label>Nom de la boutique *</Label>
                    <Input
                      required
                      placeholder="Boutique Abidjan Mode"
                      value={form.name}
                      onChange={(e) => setForm({ ...form, name: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>Slug (auto)</Label>
                    <Input
                      placeholder="boutique-abidjan-mode"
                      value={form.slug}
                      onChange={(e) => setForm({ ...form, slug: e.target.value })}
                    />
                  </div>
                </div>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label>Téléphone *</Label>
                    <Input
                      required
                      placeholder="+225 07 00 00 00 00"
                      value={form.phone}
                      onChange={(e) => setForm({ ...form, phone: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>WhatsApp Business</Label>
                    <Input
                      placeholder="+225 07 00 00 00 00"
                      value={form.whatsapp_number}
                      onChange={(e) => setForm({ ...form, whatsapp_number: e.target.value })}
                    />
                  </div>
                </div>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label>Adresse</Label>
                    <Input
                      placeholder="Rue des Jardins, Cocody"
                      value={form.address}
                      onChange={(e) => setForm({ ...form, address: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>Commune / Quartier</Label>
                    <Input
                      placeholder="Cocody"
                      value={form.commune}
                      onChange={(e) => setForm({ ...form, commune: e.target.value })}
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label>Description</Label>
                  <Input
                    placeholder="Mode et accessoires à Abidjan"
                    value={form.description}
                    onChange={(e) => setForm({ ...form, description: e.target.value })}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>Catégories (séparées par des virgules)</Label>
                  <Input
                    placeholder="Mode, Accessoires, Cosmétique"
                    value={form.categories}
                    onChange={(e) => setForm({ ...form, categories: e.target.value })}
                  />
                </div>
              </div>

              {/* Section: Moyens de paiement */}
              <div className="space-y-3 border-t pt-4">
                <div className="flex items-center gap-2">
                  <CreditCard className="size-5 text-brand" />
                  <h3 className="text-sm font-semibold">Moyens de paiement acceptés</h3>
                </div>
                <div className="flex flex-wrap gap-4">
                  {PAYMENT_MODES.map((mode) => (
                    <div key={mode.id} className="flex items-center gap-2">
                      <Checkbox
                        id={`onb-${mode.id}`}
                        checked={form.accepted_payment_modes.includes(mode.id)}
                        onCheckedChange={() => toggleMode(mode.id)}
                      />
                      <Label htmlFor={`onb-${mode.id}`} className="cursor-pointer text-sm">
                        {mode.label}
                      </Label>
                    </div>
                  ))}
                </div>
              </div>

              {/* Submit */}
              <div className="space-y-3 border-t pt-4">
                <Button type="submit" size="lg" className="w-full" disabled={submitting}>
                  {submitting ? (
                    <>
                      <Loader2 className="size-4 animate-spin" /> Création en cours…
                    </>
                  ) : (
                    <>
                      <Plus className="size-4" /> Créer ma boutique
                    </>
                  )}
                </Button>
                <p className="text-center text-xs text-muted-foreground">
                  Vous pourrez modifier ces informations plus tard dans les paramètres.
                </p>
              </div>
            </form>
          </Card>
        </motion.div>

        {/* Aperçu des étapes */}
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ delay: 0.4 }}
          className="mt-8 flex items-center justify-center gap-2 text-xs text-muted-foreground"
        >
          <span>Étapes suivantes :</span>
          {ONBOARDING_STEPS.slice(1).map((s, i) => (
            <React.Fragment key={s.title}>
              {i > 0 && <ArrowRight className="size-3" />}
              <span className="flex items-center gap-1">
                <s.icon className="size-3" />
                {s.title}
              </span>
            </React.Fragment>
          ))}
        </motion.div>
      </div>
    </div>
  );
}
