import { redirect } from "next/navigation";
export default async function EvaluationPage({params}:{params:Promise<{id:string}>}){redirect(`/runs/${(await params).id}`)}
