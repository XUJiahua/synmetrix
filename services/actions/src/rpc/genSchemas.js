import apiError from "../utils/apiError.js";
import cubejsApi from "../utils/cubejsApi.js";
import logger from "../utils/logger.js";

export default async (session, input, headers) => {
  const {
    datasource_id: dataSourceId,
    branch_id: branchId,
    tables,
    overwrite,
    format = "yaml",
  } = input || {};

  const userId = session?.["x-hasura-user-id"];

  // Log request parameters
  logger.info("genSchemas: Request received", {
    userId,
    dataSourceId,
    branchId,
    tablesCount: tables?.length || 0,
    tables,
    overwrite,
    format,
  });

  try {
    const result = await cubejsApi({
      dataSourceId,
      userId,
      authToken: headers?.authorization,
    }).generateSchemaFiles({ branchId, tables, overwrite, format });

    // Log successful response
    logger.info("genSchemas: Request completed successfully", {
      userId,
      dataSourceId,
      branchId,
      resultCode: result?.code,
      resultMessage: result?.message,
    });

    return result;
  } catch (err) {
    // Log error
    logger.error("genSchemas: Request failed", {
      userId,
      dataSourceId,
      branchId,
      error: err?.message || err,
      errorCode: err?.code,
      stack: err?.stack,
    });

    return apiError(err);
  }
};
