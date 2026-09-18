import TrialDetail from "../../runs/[id]/trial-detail";
export default async function VerificationPage({params}:{params:Promise<{id:string}>}){return <TrialDetail runID={(await params).id}/>}
