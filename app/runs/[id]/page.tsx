import TrialDetail from "./trial-detail";
export default async function Page({params}:{params:Promise<{id:string}>}){return <TrialDetail runID={(await params).id}/>}
