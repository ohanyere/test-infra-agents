import RegressionDetail from "./regression-detail";
export default async function Page({params}:{params:Promise<{id:string}>}){return <RegressionDetail comparisonID={(await params).id}/>}
