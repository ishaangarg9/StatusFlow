import Link from "next/link";
import { Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

// Static pricing placeholder. Real Stripe (test-mode) Checkout + entitlements
// land in P10; this page sets up the surface and copy.
const PLANS = [
  {
    name: "Free",
    price: "$0",
    cadence: "forever",
    description: "For side projects and trying things out.",
    features: ["Up to 3 monitors", "1 public status page", "Community support"],
    cta: "Get started",
    highlighted: false,
  },
  {
    name: "Pro",
    price: "$12",
    cadence: "/ month",
    description: "For teams that need more headroom.",
    features: [
      "Unlimited monitors",
      "Multiple status pages",
      "Private status pages",
      "Priority support",
    ],
    cta: "Start free trial",
    highlighted: true,
  },
];

export default function PricingPage() {
  return (
    <main className="mx-auto max-w-4xl px-6 py-16">
      <div className="text-center">
        <h1 className="text-3xl font-semibold">Simple pricing</h1>
        <p className="mt-2 text-muted-foreground">
          Start free. Upgrade when your team grows.
        </p>
      </div>

      <div className="mt-10 grid gap-6 sm:grid-cols-2">
        {PLANS.map((p) => (
          <Card
            key={p.name}
            className={p.highlighted ? "border-primary shadow-sm" : undefined}
          >
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>{p.name}</CardTitle>
                {p.highlighted && <Badge>Popular</Badge>}
              </div>
              <CardDescription>{p.description}</CardDescription>
              <div className="mt-2">
                <span className="text-3xl font-semibold">{p.price}</span>{" "}
                <span className="text-sm text-muted-foreground">
                  {p.cadence}
                </span>
              </div>
            </CardHeader>
            <CardContent>
              <ul className="space-y-2 text-sm">
                {p.features.map((f) => (
                  <li key={f} className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-success" />
                    {f}
                  </li>
                ))}
              </ul>
            </CardContent>
            <CardFooter>
              <Button
                asChild
                className="w-full"
                variant={p.highlighted ? "default" : "outline"}
              >
                <Link href="/signup">{p.cta}</Link>
              </Button>
            </CardFooter>
          </Card>
        ))}
      </div>

      <p className="mt-8 text-center text-xs text-muted-foreground">
        Billing is in test mode — no real charges are made.
      </p>
    </main>
  );
}
